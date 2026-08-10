<#
.SYNOPSIS
Один рой на контур: занять канонический рой, а не чеканить свой.

.DESCRIPTION
ЗАЧЕМ. Состояние роя ruflo привязано к РАБОЧЕМУ КАТАЛОГУ: любая команда, поданная
из другого cwd, заводит там свой `.hive-mind` и `.claude-flow` и считает себя
отдельным роем. Из-за этого на 05.08.2026 в контуре оказалось шесть каталогов
состояния при НУЛЕ живых процессов, и три из них рапортовали `running: true`.

ЛПР держит одну-две сессии вайб-кодинга в день намеренно: время и токены дороже
параллелизма. Два демона под две сессии — чистая потеря, тем более что рой при
этом простаивает.

ЧТО ДЕЛАЕТ. Задаёт ОДИН канонический корень роя и замок поверх него. Сессия
занимает рой, а не создаёт свой; вторая сессия видит занятость и присоединяется
или ждёт, а не поднимает второй демон.

ЧЕГО НЕ ДЕЛАЕТ. Не запускает и не останавливает демон — для этого есть
`run_task.ps1` с обязательным гейтом человека. Здесь только владение и уборка.

ЗАМОК — ЕДИНСТВЕННЫЙ ИСТОЧНИК ФАКТА «рой занят». Строка очереди в TASK-OBS-0041 —
производная запись: она говорит, что мы ОТМЕТИЛИ запуск, а занят рой или нет,
знает только замок. Поэтому замок несёт `taskId`: по нему строку можно
восстановить, а не гадать.

.PARAMETER Action
  status  — где канонический рой, жив ли он, кто держит замок
  claim   — занять рой этой сессией (или присоединиться, если уже занят живым)
  release — снять свой замок
  sweep   — найти каталоги состояния ВНЕ канона; с -Fix убрать их

.PARAMETER TaskId
  Идентификатор задачи очереди, под которую берётся замок. Пишется В ЗАМОК, чтобы
  тот, кто замок взял, мог починить пережившие отметки чужих строк (`reconcile`),
  не трогая свою собственную.

.PARAMETER NoScan
  status без обхода дисков в поисках чужих каталогов состояния. Нужен вызывающим,
  которым от status нужен только вердикт по замку: полный обход — секунды, а
  очередь спрашивает замок на каждый push.

.EXAMPLE
  .\hive_single.ps1 -Action status
  .\hive_single.ps1 -Action claim -Owner "smeta-session" -TaskId TASK-OBS-0045
  .\hive_single.ps1 -Action sweep -Fix
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('status','claim','release','sweep')][string]$Action,
    [string]$Root = 'E:\-4-\ruflo-hive',
    [string]$Owner = '',
    [string]$TaskId = '',
    [switch]$Fix,
    [switch]$IncludeForeign,
    [switch]$NoScan,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
# Путь приводится к абсолютному ДО всего остального: замок берётся через
# [System.IO.File], а .NET разрешает относительный путь от каталога процесса, а не
# от текущего каталога PowerShell. Разойдись эти два — и замок оказался бы не там,
# где его ищет очередь, то есть источник снова стал бы двумя.
if (-not [System.IO.Path]::IsPathRooted($Root)) { $Root = Join-Path (Get-Location).Path $Root }
$Root = [System.IO.Path]::GetFullPath($Root)
$LockFile = Join-Path $Root 'hive.lock.json'
$StateFile = Join-Path $Root '.claude-flow\daemon-state.json'

# Каталоги, где рой заводиться НЕ должен. Пилот исключён намеренно: он
# зафиксирован как эталон и трогать его запрещено (README моста).
$SearchRoots = @('E:\-3-\Projects', 'E:\-8-', 'E:\')
$Protected   = @('E:\-4-\ruflo-pilot')

# Каталоги ЧУЖИХ живых сессий. Показываются, но автоматически не трогаются:
# снять историю роя у работающей рядом сессии — это сломать ей работу, а не
# навести порядок. Убираются только явным -IncludeForeign.
$ForeignOwners = @('E:\-8-\comms', 'E:\-8-\air-comms')

function Out-Json($o) { $o | ConvertTo-Json -Depth 6 -Compress }

function Get-DaemonPid {
    if (-not (Test-Path -LiteralPath $StateFile)) { return $null }
    try { $s = Get-Content -LiteralPath $StateFile -Raw | ConvertFrom-Json } catch { return $null }
    $p = $s.pid
    if (-not $p -and $s.daemon)  { $p = $s.daemon.pid }
    if (-not $p -and $s.process) { $p = $s.process.pid }
    return $p
}

function Test-Alive([int]$ProcessId) {
    if (-not $ProcessId) { return $false }
    try { return $null -ne (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue) } catch { return $false }
}

function ConvertTo-Stamp($Value) {
    # Одно и то же время приходит сюда то строкой из JSON, то уже [datetime]
    # (ConvertFrom-Json сам разбирает ISO-8601). Сравнивать надо один вид, иначе
    # «время старта совпало» разошлось бы на ровном месте и живой держатель
    # объявлялся бы мёртвым.
    if ($null -eq $Value) { return $null }
    if ($Value -is [datetime]) { return $Value.ToUniversalTime().ToString('o') }
    $text = [string]$Value
    if ([string]::IsNullOrWhiteSpace($text)) { return $null }
    $parsed = [datetime]::MinValue
    if ([datetime]::TryParse($text, [System.Globalization.CultureInfo]::InvariantCulture,
                             [System.Globalization.DateTimeStyles]::RoundtripKind, [ref]$parsed)) {
        return $parsed.ToUniversalTime().ToString('o')
    }
    return $text
}

function Get-MyStart {
    try { return (Get-Process -Id $PID).StartTime.ToString('o') } catch { return $null }
}

function Test-LockAlive($Lock) {
    <#
    Жив ли держатель. Проверка — PID И ВРЕМЯ СТАРТА процесса, а не один PID:
    Windows переиспользует номера, и после перезагрузки (или просто спустя время)
    под старым PID сидит посторонний процесс. Только по PID мы бы выдали его за
    держателя и объявили рой занятым навсегда.

    Отдаёт { alive; proof }. proof = 'pid+start' — доказано обоими; 'pid-only' —
    доказано лишь PID'ом, потому что в замке нет поля pidStart.
    #>
    $processId = 0
    $proc = $null
    if ($Lock.pid -and [int]::TryParse([string]$Lock.pid, [ref]$processId) -and $processId -gt 0) {
        $proc = Get-Process -Id $processId -ErrorAction SilentlyContinue
    }
    if (-not $proc) {
        return [pscustomobject]@{ alive = $false; proof = "процесса $processId нет" }
    }
    $want = ConvertTo-Stamp $Lock.pidStart
    # Замок СТАРОГО формата (pidStart нет) — не «битый»: счесть его неизвестным
    # значило бы заклинить очередь на первом же переходе. Откатываемся на PID.
    if (-not $want) {
        return [pscustomobject]@{ alive = $true; proof = 'pid-only' }
    }
    $have = $null
    try { $have = ConvertTo-Stamp $proc.StartTime } catch { $have = $null }
    if (-not $have) {
        # Процесс есть, но время старта не отдаёт (нет прав). Считаем ЖИВЫМ:
        # ошибиться в сторону «занято» безопасно, в сторону «свободно» — нет.
        return [pscustomobject]@{ alive = $true; proof = 'pid-only (время старта процесса недоступно)' }
    }
    if ($want -eq $have) { return [pscustomobject]@{ alive = $true; proof = 'pid+start' } }
    return [pscustomobject]@{ alive = $false; proof = "PID $processId переиспользован: старт $have вместо $want" }
}

function Read-Lock {
    <#
    Три разных «нет замка» здесь НЕ склеиваются в одно, потому что решения по ним
    противоположные:
      $null      — файла нет: свободно;
      'BUSYFILE' — файл есть, но не открывается: его держит кто-то прямо сейчас
                   (claim пишет с FileShare::None) — это занятость;
      'BROKEN'   — файл прочитан, а замка в нём нет: состояние НЕИЗВЕСТНО, и выдать
                   его за «свободно» — это ровно тот отказ, из-за которого поверх
                   работающего роя выдавалась вторая задача.
    #>
    if (-not (Test-Path -LiteralPath $LockFile)) { return $null }
    try { $text = [System.IO.File]::ReadAllText($LockFile, [System.Text.Encoding]::UTF8) }
    catch { if (Test-Path -LiteralPath $LockFile) { return 'BUSYFILE' } else { return $null } }
    if ([string]::IsNullOrWhiteSpace($text)) { return 'BROKEN' }
    try { $obj = $text | ConvertFrom-Json } catch { return 'BROKEN' }
    if ($null -eq $obj -or -not $obj.pid) { return 'BROKEN' }
    return $obj
}

function New-LockJson {
    ([pscustomobject]@{
        owner    = if ($Owner) { $Owner } else { "pid-$PID" }
        pid      = $PID
        pidStart = Get-MyStart
        taskId   = $TaskId
        since    = (Get-Date).ToString('o')
        host     = $env:COMPUTERNAME
    } | ConvertTo-Json -Depth 4)
}

function New-LockFile([string]$Json) {
    <#
    Взятие замка ОДНОЙ операцией. FileMode::CreateNew — это .NET-эквивалент O_EXCL:
    файл либо создаёт ядро именно для нас, либо бросается исключение. Победитель
    ровно один, и отдельного шага «а свободно ли?» больше нет — именно тот зазор
    между чтением и записью и давал двум одновременным claim'ам общий 'claimed'.
    Слепого Set-Content поверх чужого файла в этом скрипте нет нигде.
    #>
    $stream = $null
    try {
        $stream = [System.IO.File]::Open($LockFile, [System.IO.FileMode]::CreateNew,
                                         [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
    } catch {
        # Файл на месте — значит столкнулись. Файла нет — значит отказ другой природы
        # (каталога нет, нет прав), и выдавать его за «занято» нельзя.
        if (Test-Path -LiteralPath $LockFile) { return $false }
        throw
    }
    try {
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($Json)
        $stream.Write($bytes, 0, $bytes.Length)
    } finally {
        $stream.Dispose()   # закрывать обязательно: иначе замок остаётся недописанным и занятым
    }
    return $true
}

function Find-Strays {
    # Корни поиска пересекаются (E:\ включает E:\-8-), поэтому список
    # обязательно схлопывается по пути — иначе один каталог считается дважды
    # и отчёт врёт о масштабе.
    $seen = @{}
    $found = @()
    foreach ($sr in $SearchRoots) {
        if (-not (Test-Path -LiteralPath $sr)) { continue }
        # -Depth 3: рои заводятся в корне проекта, глубже искать незачем и долго
        Get-ChildItem -LiteralPath $sr -Directory -Force -Recurse -Depth 3 `
            -Include '.hive-mind', '.claude-flow' -ErrorAction SilentlyContinue |
        ForEach-Object {
            $full = $_.FullName
            if ($seen.ContainsKey($full)) { return }
            $owner = Split-Path $full -Parent
            if ($owner -eq $Root) { return }
            if ($Protected -contains $owner) { return }
            $seen[$full] = $true
            $foreign = $ForeignOwners | Where-Object { $owner -like "$_*" }
            $found += [pscustomobject]@{
                dir = $full; project = $owner; foreign = [bool]$foreign
            }
        }
    }
    return $found
}

switch ($Action) {

'status' {
    $daemonPid = Get-DaemonPid
    $lock = Read-Lock
    # lockState — четыре РАЗЛИЧИМЫХ состояния. «Не занято» и «неизвестно» здесь
    # разные ответы: второе требует человека, а не выдачи следующей задачи.
    $lockState = 'free'; $proof = 'замка нет'
    $lockOwner = $null; $lockPid = $null; $lockSince = $null; $lockTask = $null
    if ($lock -is [string]) {
        if ($lock -eq 'BUSYFILE') { $lockState = 'held'; $proof = 'файл замка держит другой процесс — идёт взятие' }
        else                      { $lockState = 'unknown'; $proof = 'файл замка есть, но замка в нём нет' }
    } elseif ($null -ne $lock) {
        $live = Test-LockAlive $lock
        $lockState = if ($live.alive) { 'held' } else { 'stale' }
        $proof = $live.proof
        $lockOwner = $lock.owner; $lockPid = $lock.pid; $lockSince = $lock.since; $lockTask = $lock.taskId
    }
    Out-Json ([pscustomobject]@{
        root          = $Root
        rootExists    = (Test-Path -LiteralPath $Root)
        daemonPid     = $daemonPid
        daemonAlive   = (Test-Alive([int]$daemonPid))
        lockState     = $lockState
        lockOwner     = $lockOwner
        lockPid       = $lockPid
        lockSince     = $lockSince
        taskId        = $lockTask
        livenessProof = $proof
        lockAlive     = ($lockState -eq 'held')
        # замок, чей процесс мёртв, — не занятость, а мусор от прошлой сессии
        lockStale     = ($lockState -eq 'stale')
        strayDirs     = if ($NoScan) { $null } else { (Find-Strays).Count }
    })
    # Код выхода ставится явно во ВСЕХ ветках: без него $LASTEXITCODE остаётся от
    # предыдущей команды, и «замок снят» читалось бы вызывающим как отказ.
    exit 0
}

'claim' {
    if (-not (Test-Path -LiteralPath $Root)) { New-Item -ItemType Directory -Path $Root -Force | Out-Null }
    $json = New-LockJson
    $owner = if ($Owner) { $Owner } else { "pid-$PID" }
    if (New-LockFile $json) {
        Out-Json ([pscustomobject]@{
            outcome = 'claimed'; root = $Root; owner = $owner; pid = $PID; taskId = $TaskId
            replacedStaleLock = $false
        })
        exit 0
    }

    # Столкнулись. Разбираемся, ЧТО именно лежит: «занято», «мусор» и «непонятно» —
    # три разных ответа, и склеивать их нельзя.
    $lock = Read-Lock
    if ($null -eq $lock) {
        # Держатель снял замок между нашей попыткой и чтением — просто повторяем.
    } elseif ($lock -is [string] -and $lock -eq 'BUSYFILE') {
        Out-Json ([pscustomobject]@{
            outcome = 'busy'; root = $Root
            reason = 'файл замка держит другой процесс — кто-то берёт рой прямо сейчас'
        })
        exit 4
    } elseif ($lock -is [string]) {
        # BROKEN. Не «свободно» и не «занято» — отдельный код, чтобы вызывающий не
        # мог принять непрочитанный замок за отсутствующий.
        Out-Json ([pscustomobject]@{
            outcome = 'unknown'; root = $Root; lockFile = $LockFile
            reason = 'замок нечитаем или в нём нет pid — состояние роя неизвестно'
            advice = 'разобраться руками: либо рой действительно работает, либо файл — мусор'
        })
        exit 5
    } else {
        $live = Test-LockAlive $lock
        if ($live.alive -and -not $Force) {
            # Живой держатель — это НЕ повод поднимать второй рой. Присоединяемся.
            Out-Json ([pscustomobject]@{
                outcome = 'busy'; root = $Root; heldBy = $lock.owner; pid = $lock.pid
                since = $lock.since; taskId = $lock.taskId; livenessProof = $live.proof
                advice = 'рой уже занят живой сессией — работать через него, второй демон не поднимать'
            })
            exit 4
        }
        Write-Verbose "снят протухший замок $($lock.owner)/$($lock.pid): $($live.proof)"
        Remove-Item -LiteralPath $LockFile -Force -ErrorAction SilentlyContinue
    }

    # ПОВТОР РОВНО ОДИН РАЗ. Не вышло — значит замок успел взять кто-то другой, и
    # это уже занятость, а не мусор. Второго круга нет намеренно: он превратил бы
    # снятие протухшего в бесконечное перетягивание.
    if (New-LockFile $json) {
        Out-Json ([pscustomobject]@{
            outcome = 'claimed'; root = $Root; owner = $owner; pid = $PID; taskId = $TaskId
            replacedStaleLock = $true
        })
        exit 0
    }
    Out-Json ([pscustomobject]@{
        outcome = 'busy'; root = $Root
        reason = 'замок взят другим процессом между снятием протухшего и повтором'
    })
    exit 4
}

'release' {
    $lock = Read-Lock
    if ($null -eq $lock) { Out-Json ([pscustomobject]@{ outcome='noop'; reason='замка нет' }); exit 0 }
    if ($lock -is [string]) {
        # Принадлежность установить не по чему. Без -Force не трогаем: снести чужой
        # замок вслепую — это разрешить второй рой.
        if (-not $Force) {
            Out-Json ([pscustomobject]@{
                outcome='refused'; reason='замок нечитаем — чей он, неизвестно'; lockFile=$LockFile
            })
            exit 5
        }
        Remove-Item -LiteralPath $LockFile -Force
        Out-Json ([pscustomobject]@{
            outcome='released'; root=$Root; forced=$true; removedForeign=$true
            note='-Force: снят замок, принадлежность которого установить не удалось'
        })
        exit 0
    }
    # «Свой» — это тот же PID И то же время старта. Без второго условия замок
    # переиспользованного PID снимался бы посторонним процессом как свой.
    $mine = ([string]$lock.pid -eq [string]$PID)
    if ($mine) {
        $want = ConvertTo-Stamp $lock.pidStart
        $have = ConvertTo-Stamp (Get-MyStart)
        if ($want -and $have -and $want -ne $have) { $mine = $false }
    }
    if (-not $mine -and -not $Force) {
        Out-Json ([pscustomobject]@{ outcome='refused'; reason='замок принадлежит другому процессу'; lockPid=$lock.pid })
        exit 4
    }
    Remove-Item -LiteralPath $LockFile -Force
    Out-Json ([pscustomobject]@{
        outcome='released'; root=$Root; lockPid=$lock.pid; heldBy=$lock.owner
        forced=[bool](-not $mine)
        removedForeign=(-not $mine)
        note= if ($mine) { $null } else { '-Force: снят ЧУЖОЙ замок — держатель об этом не узнает' }
    })
    exit 0
}

'sweep' {
    $strays = Find-Strays
    if (-not $strays) { Out-Json ([pscustomobject]@{ outcome='clean'; moved=0 }); exit 0 }
    $mine    = @($strays | Where-Object { -not $_.foreign })
    $foreign = @($strays | Where-Object { $_.foreign })
    $moved = @()
    if ($Fix) {
        $targets = if ($IncludeForeign) { $strays } else { $mine }
        foreach ($s in $targets) {
            # ПЕРЕНОС, а не удаление: в этих каталогах лежит история прогонов
            # роя, и снести её ради порядка — потерять единственный след того,
            # что и как выполнялось.
            # У корня диска Split-Path -Leaf отдаёт «E:\», и путь назначения
            # получается недопустимым. Такой случай называется явно, а не молча
            # роняет перенос: рой в корне диска — тоже находка.
            $leaf = Split-Path $s.project -Leaf
            if ([string]::IsNullOrWhiteSpace($leaf) -or $leaf -match '[:\\/]') {
                $leaf = ($s.project -replace '[:\\/]', '_').Trim('_')
                if (-not $leaf) { $leaf = 'root' }
            }
            $dest = Join-Path (Join-Path $Root 'archive') "$leaf$(Split-Path $s.dir -Leaf)"
            try {
                New-Item -ItemType Directory -Path (Split-Path $dest -Parent) -Force | Out-Null
                if (Test-Path -LiteralPath $dest) { Remove-Item -LiteralPath $dest -Recurse -Force }
                Move-Item -LiteralPath $s.dir -Destination $dest -Force
                $moved += $dest
            } catch { Write-Warning "не удалось перенести $($s.dir): $($_.Exception.Message)" }
        }
    }
    Out-Json ([pscustomobject]@{
        outcome      = if ($Fix) { 'swept' } else { 'found' }
        found        = @($mine.dir)
        foreignHeld  = @($foreign.dir)
        moved        = $moved
        note         = if ($Fix) { 'каталоги ПЕРЕНЕСЕНЫ в archive канонического корня, не удалены' }
                       else { 'показ без правки; повторить с -Fix' }
        foreignNote  = 'каталоги чужих живых сессий не трогаются без -IncludeForeign'
    })
    exit 0
}

}
