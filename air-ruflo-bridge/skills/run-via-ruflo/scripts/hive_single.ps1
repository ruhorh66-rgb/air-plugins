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

.PARAMETER Action
  status  — где канонический рой, жив ли он, кто держит замок
  claim   — занять рой этой сессией (или присоединиться, если уже занят живым)
  release — снять свой замок
  sweep   — найти каталоги состояния ВНЕ канона; с -Fix убрать их

.EXAMPLE
  .\hive_single.ps1 -Action status
  .\hive_single.ps1 -Action claim -Owner "smeta-session"
  .\hive_single.ps1 -Action sweep -Fix
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('status','claim','release','sweep')][string]$Action,
    [string]$Root = 'E:\-4-\ruflo-hive',
    [string]$Owner = '',
    [switch]$Fix,
    [switch]$IncludeForeign,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
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

function Read-Lock {
    if (-not (Test-Path -LiteralPath $LockFile)) { return $null }
    try { return Get-Content -LiteralPath $LockFile -Raw | ConvertFrom-Json } catch { return $null }
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
    $lockAlive = if ($lock) { Test-Alive([int]$lock.pid) } else { $false }
    Out-Json ([pscustomobject]@{
        root         = $Root
        rootExists   = (Test-Path -LiteralPath $Root)
        daemonPid    = $daemonPid
        daemonAlive  = (Test-Alive([int]$daemonPid))
        lockOwner    = if ($lock) { $lock.owner } else { $null }
        lockPid      = if ($lock) { $lock.pid } else { $null }
        lockAlive    = $lockAlive
        # замок, чей процесс мёртв, — не занятость, а мусор от прошлой сессии
        lockStale    = ($null -ne $lock -and -not $lockAlive)
        strayDirs    = (Find-Strays).Count
    })
}

'claim' {
    if (-not (Test-Path -LiteralPath $Root)) { New-Item -ItemType Directory -Path $Root -Force | Out-Null }
    $lock = Read-Lock
    if ($lock) {
        $alive = Test-Alive([int]$lock.pid)
        if ($alive -and -not $Force) {
            # Живой держатель — это НЕ повод поднимать второй рой. Присоединяемся.
            Out-Json ([pscustomobject]@{
                outcome = 'busy'; root = $Root; heldBy = $lock.owner; pid = $lock.pid
                since = $lock.since
                advice = 'рой уже занят живой сессией — работать через него, второй демон не поднимать'
            })
            exit 4
        }
        if (-not $alive) { Write-Verbose "снят мёртвый замок $($lock.owner)/$($lock.pid)" }
    }
    $me = [pscustomobject]@{
        owner = if ($Owner) { $Owner } else { "pid-$PID" }
        pid   = $PID
        since = (Get-Date).ToString('o')
        host  = $env:COMPUTERNAME
    }
    $me | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $LockFile -Encoding UTF8
    Out-Json ([pscustomobject]@{
        outcome = 'claimed'; root = $Root; owner = $me.owner; pid = $me.pid
        replacedStaleLock = ($null -ne $lock)
    })
}

'release' {
    $lock = Read-Lock
    if (-not $lock) { Out-Json ([pscustomobject]@{ outcome='noop'; reason='замка нет' }); exit 0 }
    if ($lock.pid -ne $PID -and -not $Force) {
        Out-Json ([pscustomobject]@{ outcome='refused'; reason='замок принадлежит другому процессу'; lockPid=$lock.pid })
        exit 4
    }
    Remove-Item -LiteralPath $LockFile -Force
    Out-Json ([pscustomobject]@{ outcome='released'; root=$Root })
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
}

}
