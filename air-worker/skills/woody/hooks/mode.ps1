<#
.SYNOPSIS
  Включение и выключение режима оркестрации Дятла Вуди. Состояние — на сессию.

.DESCRIPTION
  Режим — это состояние на диске, а не намерение сессии. Страж хода (turn-guard.ps1)
  читает отсюда и без явного включения молчит.

  СОСТОЯНИЕ ПОСЕССИОННОЕ. Переделка 12.09.2026, решение ЛПР: «Вуди должен быть
  многопользовательским».

  Прежде файл режима был ОДИН на машину: woody-mode.json. На машине с двумя
  одновременными сессиями это давало молчаливый отказ. Первая сессия включала режим и
  занимала единственный файл, вторая под стража не попадала вообще — при том что
  состояние честно показывало «ВКЛЮЧЁН». Поймано 12.09.2026 живьём: рядом работала
  сессия d89d8733-..., её субагенты учитывались в собственном файле, а формат хода с
  неё никто не спрашивал.

  Теперь у каждой сессии свой файл, и ИМЯ ФАЙЛА И ЕСТЬ ПРИВЯЗКА:

      woody-mode-<session_id>.json               режим этой сессии
      woody-agents-<session_id>.json             её живые субагенты
      woody-mode-off-approved-<session_id>.json  согласование ЛПР на выход

  Отдельная механика самопривязки, введённая утром того же дня, этим СНЯТА за
  ненадобностью: нечего привязывать, если ключ — имя файла.

  ЧЕМ КЛЮЧ БЕРЁТСЯ. $env:CLAUDE_CODE_SESSION_ID. Это ровно то значение, которое харнесс
  кладёт в session_id события хука — проверено сверкой: привязка, записанная ЖИВЫМ
  событием Stop, совпала с переменной окружения до символа. Прежняя путаница («в
  событии третий, неизвестный идентификатор») объяснилась не расхождением, а соседней
  сессией: общий на машину файл зонда перезаписывал тот, кто заканчивал ход последним.

.EXAMPLE
  mode.ps1 -On  -Subagents 2
  mode.ps1 -Off
  mode.ps1 -Status
  mode.ps1 -All
#>
[CmdletBinding()]
param(
    [switch]$On,
    [switch]$Off,
    [switch]$Status,
    [switch]$All,
    [string]$SessionId,
    [int]$Subagents = 2,
    [string]$Forget,
    [string[]]$Reconcile,
    [switch]$ReconcileNone,
    [string]$Track,
    [string]$TrackTitle = '',
    [string]$TrackTier = '',
    [switch]$Trace,
    [int]$TraceLast = 20,
    # ПРОДУКТ ЭТОЙ СЕССИИ. Объявляется, а не угадывается — см. Set-Product ниже.
    [string]$Product,
    [switch]$ForgetProduct
)

try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch { }
$ErrorActionPreference = 'Stop'

$stateDir = Join-Path $env:ProgramData 'AIR OS\State'
if (-not (Test-Path -LiteralPath $stateDir)) { New-Item -ItemType Directory -Force -Path $stateDir | Out-Null }

$legacyFile = Join-Path $stateDir 'woody-mode.json'

# СЛЕД РЕШЕНИЙ роли «режим» (-On/-Off/-Reconcile/-Track). $ErrorActionPreference здесь
# 'Stop' — исключение из необёрнутого вызова оборвало бы саму команду режима, а не
# сгладилось «пропуском». Поэтому каждый вызов Write-WoodyTrace ниже завёрнут в свой
# try/catch: диагностика не должна иметь возможность сорвать реальное включение/
# выключение режима.
$__traceLib = Join-Path (Split-Path -Parent $PSScriptRoot) 'lib\trace.ps1'
if (Test-Path -LiteralPath $__traceLib) { . $__traceLib }
function Write-ModeTrace {
    param([string]$SessionKey, [string]$Event, [string]$Decision, [string]$Detail, [hashtable]$Extra)
    try { Write-WoodyTrace -SessionId $SessionKey -Role 'режим' -Event $Event -Decision $Decision -Detail $Detail -Extra $Extra } catch { }
}

function Get-SessionKey {
    $key = [string]$env:CLAUDE_CODE_SESSION_ID
    if ($key) { return $key }
    return $null
}

function Get-ModePath($key) { Join-Path $stateDir ("woody-mode-$key.json") }
function Get-ApprovalPath($key) { Join-Path $stateDir ("woody-mode-off-approved-$key.json") }
function Get-ProbePath($key) { Join-Path $stateDir ("woody-session-probe-$key.json") }
function Get-ProductPath($key) { Join-Path $stateDir ("woody-product-$key.json") }

# ПРОДУКТ СЕССИИ ОБЪЯВЛЯЕТСЯ, А НЕ УГАДЫВАЕТСЯ.
#
# История в две ступени, и вторая — моя собственная ошибка.
#
# Сперва страж брал продукт из рабочего каталога хода. claude-71 показала 13.09.2026, что
# каталог не удерживается между вызовами инструментов, и страж однажды предложил ей
# запустить петлю на каталоге самого скила.
#
# Тогда я привязала продукт первым, который страж увидел в сессии. Это оказалось ХУЖЕ
# исходной беды. Та же claude-71 через час: привязка поймала момент, когда она зашла в
# каталог скила ЗА ОБНОВЛЕНИЕМ (git pull), и закрепилась на нём. Дальше двигатель цели
# считал расстояние по ЧУЖОМУ состоянию, эскалация сработала на нём, и страж потребовал
# от неё закрывать шаги из PLAN.example.md — «Переписать семь скриптов», «Заполнить
# карточки», — то есть из тестового образца, не имеющего к её работе отношения.
#
# Урок точный, её же словами: механизм не видит разницы между «сессия решила работать
# здесь» и «сессия здесь просто проходила мимо». И не увидит — у прохода мимо нет
# признака, отличающего его от начала работы. Угадывание намерения надо не улучшать, а
# убирать: продукт называется ОДНОЙ КОМАНДОЙ и становится решением, а не совпадением.
#
# Формат намеренно JSON, а не строка пути. Файлы прежнего вида (просто путь) читаются как
# УСТАРЕВШИЕ и игнорируются: они могли быть записаны автопривязкой, а её решения доверия
# больше не имеют. Лучше потребовать объявить заново, чем унаследовать чужой каталог.
function Get-Product($key) {
    $p = Get-ProductPath $key
    if (-not (Test-Path -LiteralPath $p)) { return $null }
    try {
        $body = Get-Content -LiteralPath $p -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($body.declared_at -and $body.path) { return $body }
    } catch { }
    return $null
}

function Set-Product($key, [string]$path) {
    # УСПЕХ ВОЗВРАЩАЕТСЯ ФЛАГОМ, А НЕ ОБЪЕКТОМ. В PowerShell весь Write-Output функции
    # попадает в присваивание: $res = Set-Product ... захватывал строку «ОТКАЗ: …», она
    # истинна, и отказ выходил с кодом 0. Поймано первым же прогоном 13.09.2026.
    $script:productSet = $false
    if (-not $path) { return }
    if (-not (Test-Path -LiteralPath $path -PathType Container)) {
        Write-Output "ОТКАЗ: нет такого каталога — $path"
        return
    }
    $full = (Resolve-Path -LiteralPath $path).Path
    if (-not (Test-Path -LiteralPath (Join-Path $full 'run-config.json') -PathType Leaf)) {
        Write-Output "ОТКАЗ: в $full нет run-config.json — это не продукт Вуди."
        Write-Output "Если он должен им стать: woody.ps1 -Init -ProductRoot `"$full`""
        return
    }
    # Образец в продукты не берётся. Ровно на нём и споткнулась соседняя сессия: план из
    # PLAN.example.md выглядит как настоящий, и страж требовал закрывать его шаги.
    $planPath = Join-Path $full 'PLAN.md'
    if (Test-Path -LiteralPath $planPath -PathType Leaf) {
        $planText = ''
        try { $planText = Get-Content -LiteralPath $planPath -Raw -Encoding UTF8 } catch { }
        if ($planText -match 'ЗАГОТОВКА-НЕ-ЗАПОЛНЕНА') {
            Write-Output "ОТКАЗ: PLAN.md в $full — незаполненная заготовка."
            Write-Output 'Продуктом это станет, когда план опишет настоящую работу.'
            return
        }
    }
    $body = [ordered]@{
        path        = $full
        declared_at = (Get-Date).ToString('s')
        session     = $key
    }
    $body | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Get-ProductPath $key) -Encoding UTF8
    Write-Output "продукт сессии: $full"
    Write-ModeTrace $key 'вручную' 'объявлен-продукт' $full
    $script:productSet = $true
}

# Команды хуков объявлены во frontmatter скила АБСОЛЮТНЫМ путём, а регистрация скила идёт
# через <CLAUDE_CONFIG_DIR>\skills\air-woody. Пути РАЗНЫЕ, и разойтись они могут молча:
# скил зарегистрируется, а хуки будут указывать в пустоту. Найдено 12.09.2026 на второй
# машине, где каталога из frontmatter не существовало вовсе.
function Get-DeclaredHookCommands {
    $skillFile = Join-Path (Split-Path -Parent $PSScriptRoot) 'SKILL.md'
    if (-not (Test-Path -LiteralPath $skillFile)) { return @() }
    $found = @()
    foreach ($line in (Get-Content -LiteralPath $skillFile -Encoding UTF8)) {
        if ($line -match "^\s*-?\s*command:\s*'\""(.+?)\""'\s*$") { $found += $Matches[1] }
        elseif ($line -match "^\s*-?\s*command:\s*'(.+?)'\s*$") { $found += $Matches[1] }
        if ($line -match '^---\s*$' -and $found.Count -gt 0) { break }
    }
    return ($found | Select-Object -Unique)
}

# Последняя запись хука для этой сессии. Это ЕДИНСТВЕННОЕ свидетельство, что режим не
# только объявлен, но и исполняется: файл режима сам по себе ничего не доказывает.
function Get-ProbeCaptured($key) {
    $probe = Get-ProbePath $key
    if (-not (Test-Path -LiteralPath $probe)) { return $null }
    try {
        $body = Get-Content -LiteralPath $probe -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($body.captured) { return [datetime]$body.captured }
    } catch { }
    return $null
}

# «Включён» и «исполняется» — разные утверждения. Возвращаем второе.
function Get-EnforcementNote($key, $body) {
    if (-not $body.enabled) { return '' }
    $captured = Get-ProbeCaptured $key
    if (-not $captured) { return '; РЕГИСТРАЦИЯ НЕ ПОДТВЕРЖДЕНА (хук не записал ни одного хода)' }
    # enabled_at пишут только новые включения. У файлов, заведённых прежней версией, его
    # нет — тогда сверяемся с updated. Без запасного сравнения «подтверждён» встало бы и
    # там, где режим включили ПОЗЖЕ последней записи хука, то есть ровно в слепой зоне.
    $since = $null
    if ($body.enabled_at) { $since = $body.enabled_at } elseif ($body.updated) { $since = $body.updated }
    if ($since) {
        try {
            if ($captured -lt [datetime]$since) {
                return "; РЕГИСТРАЦИЯ НЕ ПОДТВЕРЖДЕНА (последняя запись хука $captured старше включения $since)"
            }
        } catch { }
    }
    # Честный предел: запись доказывает, что хук работал ТОГДА, а не что работает сейчас.
    return "; подтверждён работой хука $captured"
}

# Перенос единственного старого файла в посессионный. Делается один раз и ТОЛЬКО если
# в нём записана привязка: файл без привязки приписать некому, и угадывать здесь
# означало бы отдать чужой сессии её режим.
if (Test-Path -LiteralPath $legacyFile) {
    try {
        $legacy = Get-Content -LiteralPath $legacyFile -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($legacy.session_id) {
            $moved = Get-ModePath $legacy.session_id
            if (-not (Test-Path -LiteralPath $moved)) {
                @{
                    enabled    = [bool]$legacy.enabled
                    subagents  = $legacy.subagents
                    session_id = [string]$legacy.session_id
                    updated    = $legacy.updated
                    migrated   = (Get-Date).ToString('s')
                } | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $moved -Encoding UTF8
            }
            Remove-Item -LiteralPath $legacyFile -Force
            Write-Output "перенос: прежний общий файл режима отдан сессии $($legacy.session_id)"
        } else {
            Remove-Item -LiteralPath $legacyFile -Force
            Write-Output 'перенос: прежний общий файл режима был без привязки и снят — приписать его было некому'
        }
    } catch { }
}

# --- перечень всех сессий -----------------------------------------------------------
if ($Product -or $ForgetProduct) {
    $key = Get-SessionKey
    if ($ForgetProduct) {
        $pp = Get-ProductPath $key
        if (Test-Path -LiteralPath $pp) { Remove-Item -LiteralPath $pp -Force; Write-Output 'продукт сессии снят' }
        else { Write-Output 'продукт сессии не был объявлен' }
        exit 0
    }
    Set-Product $key $Product
    exit $(if ($script:productSet) { 0 } else { 1 })
}

if ($All) {
    $files = @(Get-ChildItem -LiteralPath $stateDir -Filter 'woody-mode-*.json' -ErrorAction SilentlyContinue)
    if ($files.Count -eq 0) { Write-Output 'режим не включён ни одной сессией'; exit 0 }
    $mine = Get-SessionKey
    foreach ($file in $files) {
        $key = $file.BaseName -replace '^woody-mode-', ''
        try { $body = Get-Content -LiteralPath $file.FullName -Raw -Encoding UTF8 | ConvertFrom-Json } catch { continue }
        $agents = Join-Path $stateDir ("woody-agents-$key.json")
        $live = 0
        if (Test-Path -LiteralPath $agents) {
            try { $live = @((Get-Content -LiteralPath $agents -Raw -Encoding UTF8 | ConvertFrom-Json).running).Count } catch { }
        }
        $mark = if ($key -eq $mine) { ' <- эта сессия' } else { '' }
        $note = Get-EnforcementNote $key $body
        Write-Output ("{0}  {1}; субагентов по настройке {2}, живых {3}; изменён {4}{5}{6}" -f `
            $key, $(if ($body.enabled) { 'ВКЛЮЧЁН' } else { 'выключен' }), $body.subagents, $live, $body.updated, $note, $mark)
        $prodAll = Get-Product $key
        Write-Output ("    продукт: {0}" -f $(if ($prodAll) { $prodAll.path } else { 'НЕ ОБЪЯВЛЕН' }))
    }
    exit 0
}

$sessionKey = Get-SessionKey

# Ключа нет — работать вслепую нельзя. Прежняя версия в этом месте молча писала общий
# файл, и получался режим «ничей»: включён, а чей — неизвестно.
if (-not $sessionKey) {
    Write-Output 'ОТКАЗ: CLAUDE_CODE_SESSION_ID пуст. Состояние режима посессионное, и без ключа его некуда писать.'
    Write-Output 'Это не сбой Вуди: переменную задаёт харнесс. Запусти из сессии Claude Code, а не из голой консоли.'
    exit 1
}

$modeFile = Get-ModePath $sessionKey
$approval = Get-ApprovalPath $sessionKey

# --- снятие мёртвой записи субагента -------------------------------------------------
# Найдено живым прогоном 12.09.2026: субагента, остановленного ЛПР, учёт не отпускает.
# SubagentStop в этом пути НЕ наступает — как и Stop при прерывании хода, — и запись
# остаётся «живой» навсегда. Страж после этого вечно требует строку на призрака, и при
# обычной работе с частыми остановками режим становится неработоспособным за полдня.
#
# Опрашивать харнесс о живости агента скрипту нечем, поэтому снятие делается явно и
# поимённо. Это НЕ обход учёта: снимается названная запись, а не список целиком, и
# только та, что уже есть в файле.
# --- след решений -----------------------------------------------------------------
# Отвечает на вопрос ЛПР «я не вижу результата ни одного стража». Результат стража —
# отсутствие события, и различить исправного от неработающего можно только следом.
if ($Trace) {
    $lib = Join-Path (Split-Path -Parent $PSScriptRoot) 'lib\trace.ps1'
    if (-not (Test-Path -LiteralPath $lib)) { Write-Output "НЕЧЕМ: нет $lib"; exit 2 }
    . $lib
    $records = @(Get-WoodyTrace -SessionId $sessionKey -Last $TraceLast)
    if ($records.Count -eq 0) {
        Write-Output "следа нет: сессия $sessionKey не записала ни одного решения."
        Write-Output 'Пустой след при включённом режиме означает, что роли не работают — это НЕ «всё спокойно».'
        exit 0
    }
    Write-Output ("след сессии {0}, последние {1} записей:" -f $sessionKey, $records.Count)
    foreach ($r in $records) {
        $extra = @()
        foreach ($p in $r.PSObject.Properties) {
            if ($p.Name -notin @('ts','role','event','decision','detail')) { $extra += ("{0}={1}" -f $p.Name, $p.Value) }
        }
        $tail = if ($extra) { '  [' + ($extra -join ', ') + ']' } else { '' }
        Write-Output ("  {0}  {1,-14} {2,-18} {3,-12} {4}{5}" -f `
            ([datetime]$r.ts).ToString('HH:mm:ss'), $r.role, $r.event, $r.decision, $r.detail, $tail)
    }
    exit 0
}

# --- учёт отцепленных ветвей ----------------------------------------------------------
# Отцепленный процесс (claude -p) мимо хуков харнесса проходит: SubagentStart на него не
# наступает, и в учёте его нет вовсе. Поймано ЛПР 12.09.2026 вопросом «не вижу, сколько
# субагентов работает» — строка состояния молчала не по забывчивости, а потому что
# показывать было нечего.
#
# У таких ветвей есть преимущество перед субагентами харнесса: их живость ПРОВЕРЯЕМА
# скриптом по идентификатору процесса. Поэтому здесь не нужен датчик-сессия, как в
# -Reconcile: мёртвые снимаются сами при любом обращении к учёту.
if ($Track) {
    $runFile = Join-Path $stateDir ("woody-agents-$sessionKey.json")
    $st = if (Test-Path -LiteralPath $runFile) {
        Get-Content -LiteralPath $runFile -Raw -Encoding UTF8 | ConvertFrom-Json
    } else {
        [pscustomobject]@{ session_id = $sessionKey; updated = $null; running = @() }
    }
    $running = @($st.running)
    if ($running | Where-Object { [string]$_.id -eq $Track }) {
        Write-Output "уже в учёте: $Track"
        Write-ModeTrace $sessionKey '-Track' 'пропустил' "$Track уже в учёте"
        exit 0
    }
    $running += [pscustomobject]@{
        id      = $Track
        type    = 'detached'
        title   = $TrackTitle
        tier    = $TrackTier
        started = (Get-Date).ToString('s')
    }
    @{ session_id = $sessionKey; updated = (Get-Date).ToString('s'); running = @($running) } |
        ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $runFile -Encoding UTF8
    Write-Output ("взято в учёт: {0} · {1} · ступень {2}" -f $Track, $TrackTitle, $TrackTier)
    Write-ModeTrace $sessionKey '-Track' 'записал' "$Track · $TrackTitle · ступень $TrackTier"
    exit 0
}

# --- сверка учёта с истиной харнесса --------------------------------------------------
# Найдено прогоном 12.09.2026: харнесс показывал ОДНОГО живого субагента, учёт — ЧЕТЫРЁХ.
# SubagentStop не срабатывает не только при остановке пользователем, но и при обычном
# завершении: учёт копит старты и теряет финалы, счёт дрейфует ВВЕРХ, и страж начинает
# требовать строку состояния на мёртвых. За полдня режим становится неработоспособным.
#
# Опросить харнесс о живости скрипту нечем — это может только сессия, инструментом
# ListAgents. Поэтому датчиком служит сессия, а сверку делает устройство: сессия передаёт
# список живых, и всё, чего в нём нет, снимается. Это не самоотчёт: список берётся у
# харнесса, а не из памяти сессии.
#
# -ReconcileNone снимает всех: отдельный ключ нужен потому, что пустой массив в -Reconcile
# неотличим от непереданного, и «живых нет» молча превратилось бы в «ничего не делать».
if ($Reconcile -or $ReconcileNone) {
    $runFile = Join-Path $stateDir ("woody-agents-$sessionKey.json")
    if (-not (Test-Path -LiteralPath $runFile)) {
        Write-Output "нечего сверять: файла учёта нет ($runFile)"
        Write-ModeTrace $sessionKey '-Reconcile' 'пропущено-нечем' 'файла учёта нет'
        exit 0
    }
    $st = Get-Content -LiteralPath $runFile -Raw -Encoding UTF8 | ConvertFrom-Json
    $before = @($st.running)
    $alive = if ($ReconcileNone) { @() } else { @($Reconcile) }
    $after = @($before | Where-Object { $alive -contains [string]$_.id })
    $dropped = @($before | Where-Object { $alive -notcontains [string]$_.id })
    $unknown = @($alive | Where-Object { $id = $_; -not ($before | Where-Object { [string]$_.id -eq $id }) })

    $st.running = $after
    $st | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $runFile -Encoding UTF8

    Write-Output ("сверка: было {0}, осталось {1}, снято {2}" -f $before.Count, $after.Count, $dropped.Count)
    foreach ($agent in $dropped) { Write-Output ("  снят: {0}  {1}  с {2}" -f $agent.id, $agent.type, $agent.started) }
    # Живой, которого нет в учёте, — это пропущенный SubagentStart. Молчать о нём нельзя:
    # страж тогда не потребует на него строки, и ветвь выпадет из отчётности незаметно.
    foreach ($id in $unknown) { Write-Output ("  ВНИМАНИЕ: харнесс знает '{0}', а учёт его не видел — пропущен SubagentStart" -f $id) }
    Write-ModeTrace $sessionKey '-Reconcile' 'записал' `
        ("было {0}, осталось {1}, снято {2}, неучтённых {3}" -f $before.Count, $after.Count, $dropped.Count, $unknown.Count)
    exit 0
}

if ($Forget) {
    $runFile = Join-Path $stateDir ("woody-agents-$sessionKey.json")
    if (-not (Test-Path -LiteralPath $runFile)) {
        Write-Output "нечего снимать: файла учёта нет ($runFile)"
        exit 0
    }
    $st = Get-Content -LiteralPath $runFile -Raw -Encoding UTF8 | ConvertFrom-Json
    $before = @($st.running)
    $after = @($before | Where-Object { [string]$_.id -ne $Forget })
    if ($before.Count -eq $after.Count) {
        Write-Output "ОТКАЗ: записи '$Forget' в учёте нет. Живых: $($before.Count)."
        foreach ($agent in $before) { Write-Output ("  {0}  {1}  с {2}" -f $agent.id, $agent.type, $agent.started) }
        exit 1
    }
    $st.running = $after
    $st | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $runFile -Encoding UTF8
    Write-Output "снята запись '$Forget'; живых осталось $($after.Count)"
    exit 0
}

# --- состояние ----------------------------------------------------------------------
if ($Status -or (-not $On -and -not $Off)) {
    if (-not (Test-Path -LiteralPath $modeFile)) {
        Write-Output "режим выключен для сессии $sessionKey (файла состояния нет)"
        exit 0
    }
    $body = Get-Content -LiteralPath $modeFile -Raw -Encoding UTF8 | ConvertFrom-Json
    Write-Output ("режим: {0}; сессия {1}; субагентов {2}; изменён {3}{4}" -f `
        $(if ($body.enabled) { 'ВКЛЮЧЁН' } else { 'выключен' }), $sessionKey, $body.subagents, $body.updated, (Get-EnforcementNote $sessionKey $body))
    # ПРОДУКТ ПЕЧАТАЕТСЯ ТАМ, КУДА СМОТРИТ ЧЕЛОВЕК. Найдено AIR-ENV-002 13.09.2026:
    # объявление стало УСЛОВИЕМ работы механизма — без него судья и двигатель не
    # запускаются вовсе, — а ни -Status, ни -All о нём не говорили ни слова. Узнать,
    # объявлен ли продукт и какой, можно было только чтением файла в ProgramData.
    #
    # И главное, её же словами: убрав автопривязку, мы убрали УГАДЫВАНИЕ, но не саму
    # ошибку — объявить не тот путь можно пальцами, и проверить за человеком по-прежнему
    # нечем. Печать — это и есть проверка: неверный путь виден тому, кто его объявил.
    $prod = Get-Product $sessionKey
    if ($prod) { Write-Output ("продукт: {0}; объявлен {1}" -f $prod.path, $prod.declared_at) }
    else { Write-Output 'продукт: НЕ ОБЪЯВЛЕН — судья и двигатель цели не запускаются. Объяви: mode.ps1 -Product "<корень>"' }
    $runFile = Join-Path $stateDir ("woody-agents-$sessionKey.json")
    if (Test-Path -LiteralPath $runFile) {
        $st = Get-Content -LiteralPath $runFile -Raw -Encoding UTF8 | ConvertFrom-Json
        # Отцепленные ветви проверяются по процессу — их живость устанавливается фактом,
        # а не доверием к записи. Мёртвые снимаются здесь же: держать их значило бы
        # повторять дрейф учёта, ради которого заведён -Reconcile.
        $alive = @()
        $reaped = @()
        foreach ($agent in @($st.running)) {
            if ([string]$agent.type -eq 'detached') {
                # Имя $pid брать НЕЛЬЗЯ: это встроенная переменная только для чтения, и
                # присваивание роняет скрипт. Поймано прогоном 12.09.2026. Страж
                # переменных этого не ловит — он различает коллизии между СВОИМИ именами,
                # а столкновение со встроенным именем для него неразличимо.
                $procId = 0
                if ([int]::TryParse([string]$agent.id, [ref]$procId) -and (Get-Process -Id $procId -ErrorAction SilentlyContinue)) {
                    $alive += $agent
                } else { $reaped += $agent }
            } else { $alive += $agent }
        }
        if ($reaped.Count -gt 0) {
            @{ session_id = $sessionKey; updated = (Get-Date).ToString('s'); running = @($alive) } |
                ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $runFile -Encoding UTF8
        }
        Write-Output ("живых субагентов: {0}" -f $alive.Count)
        foreach ($agent in $alive) {
            $extra = if ($agent.title) { "  {0}  ступень {1}" -f $agent.title, $agent.tier } else { '' }
            Write-Output ("  {0}  {1}  с {2}{3}" -f $agent.id, $agent.type, $agent.started, $extra)
        }
        foreach ($agent in $reaped) { Write-Output ("  снята мёртвая отцепленная ветвь: {0}  {1}" -f $agent.id, $agent.title) }
    }
    exit 0
}

if ($On -and $Off) { Write-Output 'ОТКАЗ: -On и -Off вместе'; exit 1 }

if ($SessionId) {
    Write-Output 'ПРЕДУПРЕЖДЕНИЕ: -SessionId проигнорирован. Ключ берётся из окружения сессии, а не от вызывающего: так исключается сам класс ошибки «назвал не ту сессию».'
}

# --- выход из режима ----------------------------------------------------------------
if ($Off) {
    # Гейт ЛПР, второй рубеж после mode-guard.ps1. Согласование названо ИМЕННО на эту
    # сессию — теперь это следует прямо из имени файла — и РАСХОДУЕТСЯ: одно
    # согласование открывает ровно один выход, а не остаётся лежать и открывать все
    # последующие.
    if (-not (Test-Path -LiteralPath $approval)) {
        Write-Output "ОТКАЗ: выход запрещён без согласования ЛПР. Нет файла $approval. Согласование кладёт ЛПР со своего терминала, не сессия."
        Write-ModeTrace $sessionKey '-Off' 'отклонил' 'нет файла согласования ЛПР'
        exit 1
    }
    $body = if (Test-Path -LiteralPath $modeFile) { Get-Content -LiteralPath $modeFile -Raw -Encoding UTF8 | ConvertFrom-Json } else { $null }
    @{
        enabled    = $false
        subagents  = $(if ($body) { $body.subagents } else { $Subagents })
        session_id = $sessionKey
        updated    = (Get-Date).ToString('s')
    } | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $modeFile -Encoding UTF8
    Remove-Item -LiteralPath $approval -Force
    Write-Output "режим выключен; сессия $sessionKey; согласование проверено, использовано и снято"
    Write-ModeTrace $sessionKey '-Off' 'записал' 'выключен, согласование ЛПР использовано и снято'
    exit 0
}

# --- включение ----------------------------------------------------------------------

# ПРЕДПОЛЁТНАЯ ПРОВЕРКА. Добавлена 12.09.2026 по находке второй машины: команды хуков
# объявлены во frontmatter АБСОЛЮТНЫМ путём, а регистрация скила идёт через
# <CLAUDE_CONFIG_DIR>\skills\air-woody — пути разные и расходятся молча. Включение при
# несуществующих командах давало файл режима, который обещает то, чего нет: страж не
# запускается, а состояние показывает ВКЛЮЧЁН. Ровно этот класс стоил суток 11-12.09.
$declared = Get-DeclaredHookCommands
if ($declared.Count -eq 0) {
    Write-Output 'ОТКАЗ: во frontmatter скила не найдено ни одной команды хука. Проверять нечем — это не «всё хорошо», а «нечем проверить».'
    Write-Output 'Ожидался SKILL.md рядом с каталогом hooks. Включение без хуков дало бы режим, который никто не держит.'
    Write-ModeTrace $sessionKey '-On' 'отклонил' 'во frontmatter скила нет ни одной команды хука'
    exit 2
}
$missing = @($declared | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) })
if ($missing.Count -gt 0) {
    Write-Output 'ОТКАЗ: режим не включён — команды хуков объявлены, но по указанным путям их нет:'
    foreach ($item in $missing) { Write-Output "  нет: $item" }
    Write-Output 'Включить сейчас значило бы записать обещание, которое некому исполнить: страж не запустится, а состояние покажет ВКЛЮЧЁН.'
    Write-Output 'Лечится тем, что путь из frontmatter должен существовать на этой машине — junction на фактический клон скила решает это, не ломая единственность источника.'
    Write-ModeTrace $sessionKey '-On' 'отклонил' ("команды хуков не найдены на диске: " + ($missing -join '; '))
    exit 1
}

@{
    enabled    = $true
    subagents  = $Subagents
    session_id = $sessionKey
    updated    = (Get-Date).ToString('s')
    enabled_at = (Get-Date).ToString('s')
    hooks_seen = @($declared)
} | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $modeFile -Encoding UTF8

Write-Output ("режим ВКЛЮЧЁН; сессия {0}; субагентов {1}; команд хуков на месте {2}" -f $sessionKey, $Subagents, $declared.Count)
Write-Output 'Проверь не этой строкой, а работой: РЕГИСТРАЦИЯ НЕ ПОДТВЕРЖДЕНА в -Status держится, пока хук не запишет ход.'
Write-ModeTrace $sessionKey '-On' 'записал' ("включён, субагентов $Subagents, команд хуков на месте $($declared.Count)")
