# run_task.ps1 — одно окно, один рой.
#
# ПЕРЕПИСАН 05.08.2026 по решению ЛПР: контур не пишет свою оркестрацию поверх
# ruflo (swarm_init/task_create вручную) — используется КАНОНИЧЕСКАЯ команда
# самого движка, `hive-mind spawn --claude -o "<цель>"`. Наша обвязка — это
# только: (1) один и тот же рой для контура, (2) гейт человека перед реальным
# запуском, (3) сканер секретов после.
#
# ОДНО ОКНО. Рой всегда живёт и запускается из ОДНОГО канонического каталога
# ($ProjectRoot). У `hive-mind spawn` нет флага «выполнить в другом каталоге» —
# состояние роя жёстко привязано к cwd вызова. Поэтому реальная работа (другой
# путь, другой проект) не переносит рой физически — целевой путь дописывается
# ТЕКСТОМ в цель, и запущенная Claude Code сессия сама переходит туда первым
# действием. Один рой — один живой процесс — одна очередь целей, как и просил ЛПР.
[CmdletBinding()]
param(
    # Цель можно передать текстом ($Objective) либо ФАЙЛОМ ($ObjectiveFile).
    # Файловый вариант заведён 08.08.2026 ради вызова из слушателя Telegram:
    # раньше тот собирал `-Objective (Get-Content "<путь>" -Raw)` строкой для
    # `powershell -Command`, то есть склеивал команду из значений заявки — и это
    # был реальный вектор инъекции (codex review, blocker). С -ObjectiveFile
    # вызывающий передаёт ПУТЬ отдельным аргументом через -File, склейки нет.
    [Parameter()][string]$Objective,
    [Parameter()][string]$ObjectiveFile,
    # Абсолютный путь, где реально лежит код/данные задачи. Рой стоит в
    # $ProjectRoot, но работает здесь — путь вписывается в текст цели.
    [Parameter(Mandatory)][string]$TargetPath,
    [Parameter()][string]$ProjectRoot = 'E:\-4-\ruflo-hive',
    [Parameter()][switch]$AllowForeignHive,
    # НЕ обязателен и передавать его не нужно: путь собирается по engine.json через
    # ruflo_engine.py. Пока он был Mandatory, версия движка жила в командной строке —
    # сохранённая ЛПР кнопка после обновления запускала старый движок молча
    # (ERR-2026-000226 повторно, 13.08.2026). Передан явно — перекрывает файл.
    [Parameter()][string]$CliPath,
    [Parameter(Mandatory)][string]$ReportPath,
    [Parameter()][ValidateSet('I_APPROVE_RUFLO_PLAN')][string]$Approval,
    # Путь к файлу подтверждённой заявки. Не обязателен (ручной прогон заявки не имеет),
    # но при запуске из слушателя передаётся всегда — и попадает В ТЕКСТ ЦЕЛИ.
    #
    # Заведён 14.08.2026 после отказа Queen на TASK-OBS-0055. Обвязка УТВЕРЖДАЛА в цели
    # «гейт пройден, кнопка нажата», а доказательства не давала: заявка лежит в
    # skill-state\ruflo-approvals, куда сессию никто не отправлял, и единственный файл,
    # который она нашла в корне роя, носил в имени `-dryrun`. Сессия отказалась брать
    # задачу — и была права по нашему же правилу «самоотчёт не доказательство».
    # Утверждение заменено ссылкой на файл, который сессия может прочитать сама.
    [Parameter()][string]$ApprovalFile,
    # Сколько воркеров завести под задачу. ДО 08.08.2026 не передавалось вовсе —
    # движок брал свой дефолт 1 (`-n, --count ... [default: 1]`), поэтому каждый
    # прогон контура шёл ОДНИМ воркером, а 18 накопленных сидели idle с нулём
    # выполненных задач. Значение 5 — из официального примера самого движка
    # (`claude-flow hive-mind spawn -n 5`), не выдумано нами.
    [Parameter()][int]$Workers = 5,
    [Parameter()][ValidateSet('normal','high','critical')][string]$Priority = 'high',
    # Идентификатор строки очереди, под которую идёт прогон. Необязателен: ручной
    # прогон очереди не касается. Уходит В ЗАМОК — по нему тот, кто замок взял,
    # чинит пережившие отметки ЧУЖИХ строк, не трогая свою (`ruflo_queue reconcile`).
    [Parameter()][string]$TaskId = '',
    # autopilot держит агентов в работе, пока не закрыты ВСЕ задачи
    # ("Persistent swarm completion"). Выключается осознанно, не по умолчанию.
    # Решение ЛПР 12.08.2026: автопилот ОСТАВИТЬ включённым — «на это я не соглашусь».
    [Parameter()][switch]$NoAutopilot,
    # Модель Queen-сессии. Умолчание берётся ИЗ ФАЙЛА НАСТРОЕК прогона, не из кода:
    # правка настройки — штатная операция, она не должна требовать версии плагина,
    # тега и доставки (замечание ЛПР 12.08.2026, справедливое: настройки лежали
    # умолчанием прямо в скрипте, и смена модели потребовала выпуска беты).
    [Parameter()][string]$QueenModel = ''
)

$ErrorActionPreference = 'Stop'

if ($ObjectiveFile) {
    if ($Objective) { throw "Указывать одновременно -Objective и -ObjectiveFile нельзя" }
    if (-not (Test-Path -LiteralPath $ObjectiveFile)) { throw "ObjectiveFile not found: $ObjectiveFile" }
    $Objective = Get-Content -LiteralPath $ObjectiveFile -Raw -Encoding UTF8
}
if (-not $Objective) { throw "Нужен -Objective или -ObjectiveFile" }

# ПОСТАНОВКА ПЕРЕДАЁТСЯ ФАЙЛОМ, А НЕ ТЕКСТОМ В ЦЕЛИ — иначе она до роя не доезжает.
#
# Замер 12.08.2026, воспроизведён на синтетической цели: движок обрезает объектив при
# сборке промпта. В сохранённом им промпте (.hive-mind\sessions\hive-mind-prompt-*.txt)
# от постановки в 6477 знаков остаются ДВЕ строки — обрыв приходится на первую запятую
# или тире внутри строки `title:`. До Node аргумент при этом доезжает целиком
# (проверено печатью process.argv), то есть режет сам движок, а не наша передача.
#
# Отсюда прогон TASK-OBS-0052, сделавший треть работы: Queen честно сообщила «текста
# задачи на диске нет» — она его и не получала, а где лежит файл, ей никто не сказал.
#
# Чиним своей стороной, а не движком: в цель кладётся ПУТЬ, читает сессия сама. Заодно
# снимается ограничение на длину постановки — теперь она любая.
# Копия кладётся рядом с отчётом ВСЕГДА, даже когда постановку подали готовым файлом.
# Прогон TASK-OBS-0053 (12.08.2026) отказался стартовать: Queen увидела приказ прочитать
# и ИСПОЛНИТЬ файл из чужого дерева (E:\-5-\...) при работе в третьем (E:\-8-\...) и
# назвала это «классическим паттерном инъекции, который стоит подтвердить, а не выполнять
# вслепую» — вывод правильный, отказ верный. Постановка в рабочем каталоге самой сессии
# такого вопроса не вызывает: 0048 часом раньше с копией в корне роя отработал молча.
# БОЕВОЙ ОТЧЁТ НЕ НАЗЫВАЕТСЯ `-dryrun`. Путь отчёта рождается на этапе заявки, и до
# 14.08.2026 реальный прогон писал в тот же файл: имя врало человеку, врало сессии
# (Queen прочла его как черновик и отказалась работать) и вдобавок затирало показанное
# ЛПР предложение — то самое, по которому он нажал кнопку. Теперь черновик остаётся,
# боевой отчёт живёт рядом с честным именем.
if ($Approval) {
    $renamed = $ReportPath -replace '-dryrun(\.[^.\\]+)$', '-run$1'
    if ($renamed -eq $ReportPath) {
        $renamed = [System.IO.Path]::Combine(
            [System.IO.Path]::GetDirectoryName($ReportPath),
            [System.IO.Path]::GetFileNameWithoutExtension($ReportPath) + "-run" +
            [System.IO.Path]::GetExtension($ReportPath))
    }
    $ReportPath = $renamed
}
$objectiveCopy = Join-Path ([System.IO.Path]::GetDirectoryName($ReportPath)) `
    ("objective-" + [System.IO.Path]::GetFileNameWithoutExtension($ReportPath) + ".md")
Set-Content -LiteralPath $objectiveCopy -Value $Objective -Encoding UTF8
$objectiveOrigin = if ($ObjectiveFile) { " Исходник постановки — $ObjectiveFile; здесь его точная копия." } else { "" }
$ObjectiveFile = $objectiveCopy
$objectiveRef = "ПОСТАНОВКА ЛЕЖИТ ФАЙЛОМ: $ObjectiveFile — ПЕРВЫМ ДЕЙСТВИЕМ прочитай его " +
    "целиком (Read) и работай по нему. В этой строке постановки НЕТ, пересказ её не " +
    "заменяет: движок обрезает длинный текст цели, поэтому он и передан файлом." +
    $objectiveOrigin +
    # Queen не видит, чем её запустили, и по описанию моста делает верный вывод, что
    # прогон без человеческого гейта — подозрительный. Поэтому гейт называется прямо.
    " Файл положен обвязкой run-via-ruflo этого же контура: прогон идёт ШТАТНЫМ путём." +
    # ДОКАЗАТЕЛЬСТВО, А НЕ УТВЕРЖДЕНИЕ. Раньше здесь стояло «гейт пройден, кнопка
    # нажата» — и всё. Сессия, обученная не верить самоотчётам, шла искать артефакт,
    # не находила его в корне роя и отказывалась работать (14.08.2026, TASK-OBS-0055).
    # Теперь называется файл, который она может прочитать сама и убедиться.
    $(if ($ApprovalFile) {
        " ОДОБРЕНИЕ ЛЕЖИТ ФАЙЛОМ: $ApprovalFile — прочитай его (Read) и убедись сам, " +
        "а не верь этой строке: в нём id заявки, sha256 подписанной постановки, " +
        "status и время решения человека. Проверять там нечего сверх этого: подпись " +
        "заявки и совпадение хеша постановки уже проверены слушателем в момент " +
        "нажатия, иначе прогон бы не начался."
    } else {
        " Заявки нет — прогон запущен вручную человеком у консоли, а не кнопкой; " +
        "проверять артефакт одобрения не нужно и негде."
    })

# Движок проверяет доступность Claude Code вызовом `execSync('which claude')`
# (dist/src/commands/hive-mind.js) — ЮНИКСОВЫЙ `which`, которого в Windows нет
# по умолчанию. Проверено 06.08.2026 дважды: ни prepend каталога с claude.exe,
# ни что-либо ещё не помогает, пока самого `which` нет в PATH — движок падает
# в тихую деградацию ("Falling back to displaying instructions"), exit 0,
# реальная сессия не запускается, а отчёт без отдельной проверки врёт "EXECUTED".
# Рабочий рецепт — из проверенного `codex-shim`-шаблона: Git for Windows везёt
# свой which.exe в usr\bin, плюс каталог с настоящим claude.exe для самого
# исполнения (не только для проверки).
$GitBin      = 'C:\Program Files\Git\usr\bin'
$ClaudeExeDir = 'C:\Users\admin_loc\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\bin'
if ((Test-Path -LiteralPath (Join-Path $GitBin 'which.exe')) -and (Test-Path -LiteralPath (Join-Path $ClaudeExeDir 'claude.exe'))) {
    $env:Path = "$GitBin;$ClaudeExeDir;$env:Path"
} else {
    Write-Warning "which.exe ($GitBin) или claude.exe ($ClaudeExeDir) не найдены — реальный запуск, скорее всего, откажет так же, как раньше"
}

# Версия движка — ОДНО число в engine.json, и оно решает всё: путь спавна, путь
# MCP-сервера, отчёт. Разрешатель заодно приводит .mcp.json к той же версии и к имени
# сервера ruflo — иначе координация и запуск снова разъедутся по версиям.
$engineScript = Join-Path $PSScriptRoot 'ruflo_engine.py'
$engineSync = ''
if (Test-Path -LiteralPath $engineScript) {
    $engineSync = (& python $engineScript --sync-mcp 2>&1 | Out-String).Trim()
    if (-not $CliPath) {
        $resolved = (& python -c "import sys; sys.path.insert(0, r'$PSScriptRoot'); import ruflo_engine; print(ruflo_engine.cli_path())" 2>&1 | Out-String).Trim()
        if ($LASTEXITCODE -ne 0 -or -not $resolved) { throw "движок не определён: $resolved" }
        $CliPath = $resolved
    }
}
if (-not $CliPath) { throw "Нужен -CliPath: ruflo_engine.py недоступен, определить движок нечем" }
if (-not (Test-Path -LiteralPath $CliPath)) { throw "CLI not found: $CliPath" }
if (-not (Test-Path -LiteralPath $TargetPath)) { throw "TargetPath not found: $TargetPath" }
$CanonicalHive = 'E:\-4-\ruflo-hive'
if (-not (Test-Path -LiteralPath $ProjectRoot)) { New-Item -ItemType Directory -Path $ProjectRoot -Force | Out-Null }
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).Path
$TargetPath  = (Resolve-Path -LiteralPath $TargetPath).Path

# ЖЁСТКИЙ ЗАПРЕТ — не отменён этой правкой, один рой остаётся один.
if ($ProjectRoot -ne $CanonicalHive -and -not $AllowForeignHive) {
    [pscustomobject]@{
        outcome='refused'; reason='рой заводится только в каноническом корне'
        requested=$ProjectRoot; canonical=$CanonicalHive
        advice='убрать -ProjectRoot, либо осознанно передать -AllowForeignHive'
    } | ConvertTo-Json -Compress
    exit 5
}

$reportDirectory = Split-Path -Parent $ReportPath
if ($reportDirectory -and -not (Test-Path -LiteralPath $reportDirectory)) { New-Item -ItemType Directory -Path $reportDirectory -Force | Out-Null }
$transcript = [System.Collections.Generic.List[string]]::new()

# НАСТРОЙКИ ПРОГОНА — ФАЙЛОМ, А НЕ ВЕРСИЕЙ ПЛАГИНА. Указание ЛПР 12.08.2026:
# «настройки — это штатная операция». Пока умолчания лежали в коде, смена модели
# требовала бампа версии, тега и доставки — то есть выпуска ради настройки.
#
# Порядок старшинства: явный параметр вызова > файл настроек > встроенное умолчание.
# Файла нет — работаем на встроенных и НАЗЫВАЕМ это в отчёте, а не молчим.
$runConfigPath = Join-Path $ProjectRoot 'run-config.json'
$runConfig = $null
if (Test-Path -LiteralPath $runConfigPath) {
    try { $runConfig = Get-Content -LiteralPath $runConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json }
    catch { $transcript.Add("## Настройки прогона`nФАЙЛ НЕ РАЗОБРАН: $runConfigPath — $($_.Exception.Message). Работаю на встроенных умолчаниях.") }
}
if (-not $QueenModel) {
    $QueenModel = if ($runConfig -and $runConfig.queen_model) { $runConfig.queen_model } else { 'sonnet' }
}
if (-not $PSBoundParameters.ContainsKey('Workers') -and $runConfig -and $runConfig.workers) {
    $Workers = [int]$runConfig.workers
}
if (-not $NoAutopilot -and $runConfig -and $runConfig.PSObject.Properties.Name -contains 'autopilot' -and -not $runConfig.autopilot) {
    $NoAutopilot = $true
}
# ЭСКАЛАЦИЯ НА ДОРОГУЮ МОДЕЛЬ — ПО НАЗВАННОЙ ПРИЧИНЕ И С ПОТОЛКОМ.
# Решение ЛПР 13.08.2026. Замер, из которого оно выросло: в прогоне TASK-OBS-0053 у
# координатора 149 ответов из 566 по всему прогону — три четверти объёма делают воркеры,
# и перевод ВСЕХ на opus умножил бы самую объёмную часть. Дорого не суждение, дорого
# чтение кода. Поэтому Queen идёт на дорогой модели, воркеры на дешёвой, а opus приходит
# к воркеру только по названному условию и не чаще потолка.
$workerModel = if ($runConfig -and $runConfig.escalation -and $runConfig.escalation.worker_model) { $runConfig.escalation.worker_model } else { 'sonnet' }
$maxOpus = if ($runConfig -and $runConfig.escalation -and $runConfig.escalation.max_opus_calls) { [int]$runConfig.escalation.max_opus_calls } else { 0 }
$triggers = @()
if ($runConfig -and $runConfig.escalation -and $runConfig.escalation.triggers) { $triggers = @($runConfig.escalation.triggers) }

$transcript.Add("## Настройки прогона`nисточник: " +
    $(if ($runConfig) { "$runConfigPath (правка — штатная операция, версия плагина не поднимается)" } else { "файла нет, встроенные умолчания" }) +
    "`n- модель Queen: $QueenModel`n- модель воркеров: $workerModel`n- воркеров: $Workers`n- автопилот: " +
    $(if ($NoAutopilot) { 'выключен' } else { 'включён' }) +
    "`n- совет роутера со смещением в стоимость: " +
    $(if ($runConfig -and $runConfig.prefer_cost) { 'да' } else { 'нет' }) +
    "`n- потолок обращений к opus: " + $(if ($maxOpus -gt 0) { $maxOpus } else { 'не задан' }) +
    $(if ($triggers.Count) { "`n- условия эскалации:`n  - " + ($triggers -join "`n  - ") } else { '' }))

# МОДЕЛЬ QUEEN-СЕССИИ. Замер 12.08.2026 показал главное: все сессии роя за всё время —
# 100% claude-opus-5, включая тех, кого Queen раздавала «на sonnet». Причина не в
# задаче: рой запускается обычной сессией Claude Code и НАСЛЕДУЕТ настройку машины
# (settings.json: model = opus[1m]). Роутер при этом оценивает среднюю сложность задач
# в 23,7% — то есть почти всё это работа для sonnet.
#
# Модель задаётся переменной среды на ВРЕМЯ ПРОГОНА: настройку машины не трогаем —
# у неё другие потребители, и менять поведение всего контура ради одного прогона
# значит чинить не там (решение ЛПР 12.08.2026: перевести Queen, автопилот оставить).
if (-not $env:AIR_RUFLO_KEEP_MODEL) {
    $env:ANTHROPIC_MODEL = $QueenModel
    $transcript.Add("## Модель прогона`nQueen-сессия запускается на ``$QueenModel`` (переменная ANTHROPIC_MODEL на время прогона; настройка машины не менялась).")
}
$transcript.Add("## Движок`n$CliPath`nMCP-регистрация: $engineSync")

# --- Замок: ОДИН на контур, и он же единственный источник факта «рой занят» ----
#
# КОРЕНЬ ЗАМКА — НЕ $ProjectRoot. Раньше замок брался в -Root $ProjectRoot, а
# `ruflo_queue.py` спрашивал его в своём REPORTS (E:\-4-\ruflo-hive). Пока
# $ProjectRoot совпадал с каноническим, разницы не было видно; с
# -AllowForeignHive это ДВА РАЗНЫХ ФАЙЛА, то есть два независимых замка, и
# «единый источник» перестаёт существовать ровно в том сценарии, ради которого
# флаг и заведён. Берём один корень с очередью и подменяем его той же
# переменной окружения — иначе полигон снова разъедет обе стороны.
$LockRoot = if ($env:RUFLO_REPORTS) { $env:RUFLO_REPORTS } else { $CanonicalHive }
$hiveSingle = Join-Path $PSScriptRoot 'hive_single.ps1'
if (Test-Path -LiteralPath $hiveSingle) {
    $claim = & $hiveSingle -Action claim -Root $LockRoot -Owner "run_task-$PID" -TaskId $TaskId 2>$null
    # Живой держатель (4) и нечитаемый замок (5) — оба отказ, и оба возвращаются
    # вызывающему как есть: он должен видеть, ЧТО именно случилось, а не общий «1».
    if ($LASTEXITCODE -ne 0) { $claim; exit $LASTEXITCODE }
    $transcript.Add("## Замок роя`n~~~json`n$claim`n~~~")

    # ПОЧИНКА СТРОК — ПРАВО ТОГО, КТО ВЗЯЛ ЗАМОК, и только его. Отметка `approved`,
    # пережившая свой прогон (падение между взятием замка и записью строки),
    # чинится здесь: замок только что взят нами, значит рой этими строками не занят.
    # Раньше это делал любой, кто заглянул в очередь, — и снимал отметку с прогона,
    # который в этот момент шёл.
    $queueScript = Join-Path $PSScriptRoot 'ruflo_queue.py'
    $python = (Get-Command python -ErrorAction SilentlyContinue).Source
    if ($python -and (Test-Path -LiteralPath $queueScript)) {
        try {
            # Без -Approval это ПОКАЗ заявки, а не прогон: строку в approved не двигаем.
            $recArgs = @($queueScript, 'reconcile', $PID)
            if (-not $Approval) { $recArgs += '--dry-run' }
            $rec = & $python @recArgs 2>&1 | Out-String
            $transcript.Add("## Очередь: reconcile`n~~~text`n$rec`n~~~")
        } catch {
            # Очередь недоступна — прогон из-за этого не отменяем: замок уже наш,
            # и занятость роя от строки таблицы не зависит.
            $transcript.Add("## Очередь: reconcile НЕ ВЫПОЛНЕН`n$($_.Exception.Message)")
        }
    }
}

# --- Дефект отчёта, найден и закрыт 08.08.2026 ------------------------------
# Симптом: реальный прогон упал на четвёртом шаге, а отчёт оборвался на третьем —
# причину (протухший OAuth в Claude Code) пришлось искать руками, повторяя вызов
# в консоли. Корень двойной:
#   1) $ErrorActionPreference = 'Stop': когда node пишет в stderr, PowerShell
#      бросает исключение ДО присваивания переменной, и вывод шага теряется целиком;
#   2) транскрипт писался в файл лишь на некоторых ветках, а не на всех.
# Лечится здесь: вызовы движка идут через Invoke-Ruflo (вывод и код возвращаются
# всегда, исключение не бросается), а запись отчёта вынесена в Save-Report и
# вызывается в finally безусловно.
function Invoke-Ruflo {
    param([Parameter(Mandatory)][string[]]$Arguments, [Parameter(Mandatory)][string]$Stage)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = & node $CliPath @Arguments 2>&1 | Out-String
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $prev
    }
    $transcript.Add("## $Stage`n~~~text`n$out`n~~~`nexit: $code")
    [pscustomobject]@{ Output = $out; ExitCode = $code }
}

$script:reportSaved = $false
function Save-Report {
    try {
        $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
        $script:reportSaved = $true
    } catch {
        Write-Warning "не удалось записать отчёт $ReportPath : $($_.Exception.Message)"
    }
}

Push-Location $ProjectRoot
try {
    # hive-mind init один раз на канонический рой — идемпотентно по факту наличия .hive-mind
    if (-not (Test-Path -LiteralPath (Join-Path $ProjectRoot '.hive-mind'))) {
        $initOut = & node $CliPath hive-mind init -t hierarchical-mesh 2>&1 | Out-String
        $transcript.Add("## hive-mind init`n~~~text`n$initOut`n~~~")
        if ($LASTEXITCODE -ne 0) { throw "hive-mind init failed ($LASTEXITCODE)" }
    }

    # Путь, по которому рой обязан положить снятые метрики, и по которому приёмка их
    # ищет: одно имя в двух местах разошлось бы на первой же правке.
    $metricsPath = Join-Path ([System.IO.Path]::GetDirectoryName($ReportPath)) "metrics-$TaskId.json"

    # Проверка регистрации MCP — ОДИН РАЗ на канонический каталог (не на TargetPath:
    # рой всегда запускается из $ProjectRoot, поэтому регистрация нужна только здесь).
    # Без неё `--claude` тихо запускает обычную дорогую сессию БЕЗ mcp__claude-flow__*
    # инструментов — задокументированный отказ (005_Ruflo_Wiki, Пилот 04.08.2026).
    $mcpConfigured = $false
    $mcpJson = Join-Path $ProjectRoot '.mcp.json'
    if (Test-Path -LiteralPath $mcpJson) {
        try {
            $cfg = Get-Content -LiteralPath $mcpJson -Raw | ConvertFrom-Json
            # Имя сервера задаёт ПРЕФИКС инструментов, и движок в своём промпте требует
            # именно mcp__ruflo__*. Старое имя claude-flow принимаем как рабочее, но
            # называем расхождение: при нём Queen получает приказ на инструменты,
            # которых у неё нет (отказ TASK-OBS-0053 12.08.2026).
            $mcpConfigured = [bool]($cfg.mcpServers.'ruflo' -or $cfg.mcpServers.'claude-flow')
            if (-not $cfg.mcpServers.'ruflo' -and $cfg.mcpServers.'claude-flow') {
                $transcript.Add("## MCP registration`nСервер зарегистрирован именем claude-flow, а движок требует mcp__ruflo__*. Переименовать в $mcpJson.")
            }
            $mcpCli = @($cfg.mcpServers.'ruflo'.args + $cfg.mcpServers.'claude-flow'.args) | Where-Object { $_ -like '*cli.js' } | Select-Object -First 1
            if ($mcpCli -and $mcpCli -ne $CliPath) {
                $transcript.Add("## MCP registration`nMCP-сервер поднимается движком $mcpCli, а спавн идёт $CliPath — координация и запуск на разных версиях (ERR-2026-000226).")
            }
        } catch {}
    }
    if (-not $mcpConfigured) {
        $transcript.Add("## MCP registration`nНЕ НАЙДЕНА в $mcpJson. Без неё --claude запустит сессию без инструментов роя.")
    }

    # Цель роя = абсолютный путь работы + ТРЕБОВАНИЕ ДЕЛЕГИРОВАТЬ + сама задача.
    # Рой стоит в $ProjectRoot, спавненная сессия обязана сама перейти в $TargetPath.
    #
    # Требование делегирования дописывается ОБВЯЗКОЙ, а не автором цели
    # (ERR-2026-000194). Прогон 08.08.2026 прошёл все четыре шага, дал результат —
    # и при этом Queen не сделала НИ ОДНОГО вызова mcp__claude-flow__*, хотя все
    # 334 инструмента роя были ей доступны: сделала всё сама (Edit 45, Bash 31).
    # Причина: цель перечисляла ЧТО сделать, но не требовала РАСПРЕДЕЛИТЬ. Пока
    # это требование живёт в голове у автора цели, оно будет забываться — здесь
    # оно добавляется к каждой цели механически.
    $delegation = "КАК РАБОТАТЬ (обязательно, это рой, а не одиночная сессия): " +
        "тебе доступны инструменты mcp__ruflo__* — используй их. Разбей задачу " +
        "на подзадачи и раздай воркерам через mcp__ruflo__agent_spawn и " +
        "mcp__ruflo__task_assign, координируй через mcp__ruflo__ " +
        "(memory_store/search для общего контекста, swarm_status для контроля). " +
        "Делать всю работу самой обычными Edit/Bash/Write вместо распределения — " +
        "прямое нарушение постановки: приёмка прогона включает проверку лога на " +
        "фактические вызовы mcp__ruflo__, и прогон без них считается " +
        "несостоявшимся независимо от того, получен ли предметный результат. " +
        "Спавни воркеров под РОЛИ задачи (coder, tester, reviewer, architect, " +
        "researcher), а не безымянных worker. " +
        # Изоляция worktree отсюда НЕ РАБОТАЕТ и стоит прогона: 13.08.2026 два первых
        # Task-вызова упали с «Cannot create agent worktree: not in a git repository»,
        # потому что рой запускается из E:\-4-\ruflo-hive, а это не репозиторий. Упавшие
        # вызовы при этом заняли слоты у ограничителя одновременных исполнителей, и все
        # следующие раздачи он отверг — прогон встал, не сделав ни строчки.
        # Дешёвое по умолчанию, дорогое — по названной причине и со счётом.
        "МОДЕЛЬ ИСПОЛНИТЕЛЯ: раздавай воркерам `$workerModel. Дорогая модель (opus) " +
        "приходит ТОЛЬКО по одному из названных условий: " + ($(if ($triggers.Count) { ($triggers -join '; ') } else { 'условия не заданы — эскалации нет' })) +
        ". Потолок обращений к opus за прогон: $maxOpus. У КАЖДОГО обращения к opus в " +
        "отчёте называется причина — какое из условий сработало. Эскалация «на всякий " +
        "случай», «задача выглядит сложной», «чтобы наверняка» запрещена: за неё платит " +
        "не прогон, а весь контур, и именно так доля opus доходила до 88 процентов. " +
        "Потолок исчерпан, а условие снова сработало — доделывай на дешёвой модели и " +
        "НАЗОВИ это в отчёте недостигнутым, а не превышай молча. " +
        "ИЗОЛЯЦИЮ WORKTREE НЕ ПРОСИТЬ (isolation: worktree): рабочий каталог этой " +
        "сессии не git-репозиторий, вызов упадёт и ЗАНЯТ СЛОТ у ограничителя " +
        "одновременных исполнителей. Параллельность обеспечивается тем, что каждый " +
        "воркер правит свои файлы, а не общим деревом. " +
        # Предел стоит на КАЖДОМ вызове Agent во всём контуре, а не только у роя.
        "ОДНОВРЕМЕННЫХ ИСПОЛНИТЕЛЕЙ НЕ БОЛЬШЕ ДВУХ — предел контура " +
        "(AIR_VIBECODING § 10). Раздавай по два и дожидайся возврата, а не веером: " +
        "третий вызов будет отвергнут, и это не ошибка, а правило. " +
        # Маршрутизатор моделей у движка есть и самообучается, но он НЕ перехватывает
        # сам: его надо спросить. За девять прогонов 08-11.08.2026 его не звали ни
        # разу, и потому Opus сделал 88% всех ответов. Бандит корректируется примерно
        # после 50 исходов — без вызовов он их не накопит никогда.
        "ПЕРЕД тем как раздать подзадачу, спрашивай совет по МОДЕЛИ: " +
        # Обратные кавычки вокруг команды НЕ ставить: в двойных кавычках PowerShell это
        # escape-символ, и `r превращался в возврат каретки — рой получал в цели
        # «CLI: <CR>uflo hooks model-route», то есть команду без имени.
        "mcp__ruflo__hooks_model-route (CLI: ruflo hooks model-route -t <задача>) " +
        "— именно model-route. Не путать с hooks_route: тот маршрутизирует ТИП АГЕНТА " +
        "и модели не возвращает вовсе. Спрашивай совет СО СМЕЩЕНИЕМ В СТОРОНУ " +
        "СТОИМОСТИ: у CLI это флаг --prefer-cost; если у инструмента MCP такого " +
        "параметра нет, скажи об этом в отчёте и спроси совет как есть — выдумывать " +
        "параметр не нужно. " +
        # ШТАТНЫЙ СПОСОБ ИСПОЛНЕНИЯ — Task с моделью, так написано в USERGUIDE движка:
        # «Pass model="haiku" to Task tool for cost savings». Рекомендация НЕ применяется
        # сама, её обязан передать вызывающий. Прежняя редакция требовала «спавни агента
        # с посоветованной моделью» — и это не работало: agent_spawn только РЕГИСТРИРУЕТ
        # агента (status: registered), а agent_execute ходит в Anthropic Messages API по
        # ANTHROPIC_API_KEY, которого у контура нет (замер 11.08.2026: fetch failed за
        # 3-14 мс). Совет спрашивали, модель записывали, работу делала Queen на Opus.
        "ИСПОЛНЯЙ подзадачу инструментом Task, передавая ему посоветованную модель " +
        "параметром model (например model=haiku): рекомендация роутера сама не " +
        "применяется, её передаёт вызывающий. agent_spawn при этом остаётся для УЧЁТА " +
        "и координации роя, но работы он не исполняет. " +
        "Тяжёлые куски кода можно отдавать воркеру Codex одной командой Bash: " +
        "codex-companion.mjs task --background --write '<кусок>', затем status/result " +
        "по job-id — это вторая платформа, она не тратит нашу подписку. " +
        "Совет роутера НЕ обязателен к исполнению — он вероятностный и на малой " +
        "статистике ошибается; если противоречит существу задачи, бери свою модель и " +
        "скажи в отчёте, почему. " +
        "ПОСЛЕ завершения подзадачи ОБЯЗАТЕЛЬНО запиши исход: " +
        "mcp__ruflo__hooks_model-outcome с задачей, использованной моделью и " +
        "исходом (success/failure/escalated). Без этой второй половины роутер НЕ " +
        "учится: приоры обновляет именно запись исхода, а не сам запрос совета. " +
        # Метрики — не украшение постановки, а то, чем рой меряет СЕБЯ по ходу дела.
        # Без машинного файла их не читает никто, и прогон закрывается «сделано» при
        # нуле сдвинутых порогов (TASK-OBS-0053, 13.08.2026).
        "МЕТРИКИ ПОСТАНОВКИ — ОБЯЗАТЕЛЬНЫЙ АРТЕФАКТ. В постановке есть таблица метрик " +
        "«было → порог». ПЕРЕД завершением сними каждую из них ЗАНОВО (из базы, из " +
        "репозитория, живым вызовом — тем же способом, что назван в постановке) и " +
        "положи файл $metricsPath строго такого вида: " +
        '{"task":"<TaskId>","metrics":[{"name":"<метрика>","before":"<было>",' +
        '"threshold":"<порог>","after":"<стало>","reached":true|false,' +
        '"how":"<чем снято>"}]}. ' +
        "Ставь reached=false честно: недостигнутый порог с названной причиной — " +
        "нормальный исход, а приписанный — подлог, который вскроется на первой же " +
        "проверке человеком. Файла нет — прогон не засчитывается как закрытие задачи, " +
        "сколько бы работы ни было сделано. Сверяйся с этой таблицей ПО ХОДУ, а не в " +
        "конце: она и есть ответ на вопрос «что ещё осталось»."
    # Порядок частей не случаен: путь к постановке идёт ПЕРВЫМ, до наставлений о том,
    # как работать. Обрыв движка приходится на начало строки, и если что-то и уцелеет,
    # это должно быть указание, где лежит задача.
    $fullObjective = "$objectiveRef РАБОЧИЙ КАТАЛОГ ЗАДАЧИ: $TargetPath — перейди туда (cd) или используй " +
        "только абсолютные пути на каждую файловую/shell операцию. Эта координирующая сессия " +
        "запущена из $ProjectRoot (канонический рой контура) — файлы этого каталога НЕ ТРОГАТЬ, " +
        "он не относится к задаче. $delegation"

    $dry = Invoke-Ruflo -Arguments @('hive-mind','spawn','--claude','-o',$fullObjective,'--dry-run') -Stage 'Dry run'
    $planned = "ЧТО БУДЕТ ЗАПУЩЕНО ПРИ -Approval (четыре шага, не один):`n" +
        "  1. hive-mind spawn -n $Workers        — завести $Workers воркеров под задачу`n" +
        "  2. hive-mind task -d <цель> -p $Priority   — положить задачу в очередь роя`n" +
        "  3. autopilot enable                   — $(if ($NoAutopilot) {'ОТКЛЮЧЕНО флагом -NoAutopilot'} else {'держать агентов до закрытия ВСЕХ задач'})`n" +
        "  4. hive-mind spawn --claude -o <цель> — Queen-сессия, раздаёт работу через MCP`n"
    $transcript.Insert(0, "# Ruflo proposal (canonical hive-mind spawn)`n`nTargetPath: $TargetPath`nProjectRoot: $ProjectRoot`nMCP registered: $mcpConfigured`n`n$planned")
    if ($dry.ExitCode -ne 0) { throw "hive-mind spawn --dry-run failed ($($dry.ExitCode))" }

    if (-not $Approval) {
        $transcript.Add("## Human gate`nSTOPPED: proposal above must be shown to a human. Re-run only with -Approval I_APPROVE_RUFLO_PLAN. Claude Code was NOT launched.")
        Save-Report
        Write-Output "STOPPED FOR HUMAN APPROVAL. Report: $ReportPath"
        exit 10
    }

    # БЮДЖЕТ ПРОВЕРЯЕТСЯ ДО ЗАПУСКА, А НЕ ПОСЛЕ. Первый настоящий предел расхода в
    # контуре: `budget check` плагина учёта возвращает 1 при исчерпании лимита, и тогда
    # прогон не начинается. До 12.08.2026 предела не было вовсе — прогон мог стоить
    # сколько угодно, и узнавали об этом постфактум.
    #
    # Лимит не задан — не препятствие: команда молчит, прогон идёт. Отказ бюджета
    # отличается от отказа проверки: не сработавшая проверка НЕ считается разрешением,
    # но и не блокирует (лимита может просто не быть).
    try {
        $trackerRoot = Get-ChildItem (Join-Path $env:USERPROFILE '.claude\plugins\cache\ruflo\ruflo-cost-tracker') `
            -Directory -ErrorAction Stop | Sort-Object LastWriteTime -Descending | Select-Object -First 1
        $budget = Join-Path $trackerRoot.FullName 'scripts\budget.mjs'
        if (Test-Path -LiteralPath $budget) {
            Push-Location $ProjectRoot
            # Предел ДНЕВНОЙ, а не «за всю историю»: иначе накопленный расход прошлых
            # недель однажды закрывает работу навсегда, и предел приходится снимать —
            # то есть он перестаёт быть пределом.
            $env:BUDGET_PERIOD = 'today'
            $budgetOut = & node $budget check 2>&1 | Out-String
            $budgetCode = $LASTEXITCODE
            Pop-Location
            if ($budgetCode -eq 1) {
                $transcript.Add("## Бюджет исчерпан — прогон НЕ начат`n~~~text`n$($budgetOut.Trim())`n~~~")
                Save-Report
                Write-Output "REFUSED: бюджет исчерпан, прогон не начинался. Отчёт: $ReportPath"
                exit 11
            }
            $transcript.Add("## Бюджет перед запуском`n~~~text`n$($budgetOut.Trim())`n~~~")
        }
    } catch {
        $transcript.Add("## Бюджет перед запуском`nПРОВЕРИТЬ НЕ СМОГ: $($_.Exception.Message). Прогон идёт: отсутствие лимита не есть его превышение.")
    }

    if (-not $mcpConfigured) {
        $transcript.Add("## Refused`nMCP не зарегистрирован для $ProjectRoot — реальный запуск дал бы одну дорогую сессию без роя, не запускаю. Разово: claude mcp add -s project claude-flow -- node `"$CliPath`" mcp start (из $ProjectRoot), затем одобрить в ~/.claude.json.")
        Save-Report
        Write-Output "REFUSED: MCP not registered for $ProjectRoot. Report: $ReportPath"
        exit 6
    }

    # --- Реальный запуск: ТРИ шага, а не один -------------------------------
    # Найдено 08.08.2026 разбором справки самого движка после указания ЛПР
    # «не придумывать, смотреть первоисточник» (ERR-2026-000192). До этой правки
    # вызывался ТОЛЬКО третий шаг, поэтому: воркеров всегда 1 (дефолт движка),
    # очередь задач роя пустая, координатору некого координировать и нечего
    # раздавать. Статистика подтвердила: 18 воркеров, все idle, Completed=0.
    #
    #   1) spawn -n N     — завести воркеров под задачу
    #   2) task -d "цель" — положить задачу В ОЧЕРЕДЬ РОЯ (`Submit tasks to the hive`)
    #   3) spawn --claude — поднять Queen-сессию, которая раздаёт работу через MCP

    # Отметка старта нужна приёмке метрик ниже: файл, лежащий здесь ДО начала работы,
    # остался от прошлого прогона и доказательством этого прогона не является.
    $runStartedAtUtc = [DateTime]::UtcNow

    $r1 = Invoke-Ruflo -Arguments @('hive-mind','spawn','-n',"$Workers") -Stage "1. Spawn workers (-n $Workers)"
    if ($r1.ExitCode -ne 0) { throw "hive-mind spawn -n $Workers failed ($($r1.ExitCode))" }

    # ОЧЕРЕДИ РОЯ — КОРОТКАЯ строка, Queen-сессии — полная. Это разные адресаты.
    #
    # Установлено разбором 08.08.2026 (ERR-2026-000214, вторая, верная итерация).
    # `task_create` в движке валидирует вход ПЕРВЫМ делом:
    #     validateText(input.description, 'description')   // maxLen = 10 000
    # и при отказе возвращает { success:false, error } — объект БЕЗ `assignedTo`.
    # Печать результата затем читает `result.assignedTo.length` и падает. То есть
    # «TypeError: Cannot read properties of undefined» означает не дефект движка,
    # а НАШУ слишком длинную строку: 9 129 знаков задания плюс преамбула обвязки.
    #
    # Прошлый удачный прогон (TASK-OBS-0040, 6 753 знака) укладывался — потому и
    # выглядело, будто дело в приоритете. Это была ложная связь.
    #
    # Очереди достаточно опознавательной строки: полное задание всё равно получает
    # Queen-сессия шагом 4, и оно же лежит файлом, который она читает.
    $queueLine = "$($TargetPath): " + ($Objective -replace '\s+', ' ')
    if ($queueLine.Length -gt 9000) { $queueLine = $queueLine.Substring(0, 9000) + ' […полное задание — шаг 4 и файл цели]' }
    $r2 = Invoke-Ruflo -Arguments @('hive-mind','task','-d',$queueLine,'-p',$Priority) -Stage "2. Submit task to hive queue (-p $Priority)"
    if ($r2.ExitCode -ne 0) { throw "hive-mind task failed ($($r2.ExitCode))" }

    if (-not $NoAutopilot) {
        $r3 = Invoke-Ruflo -Arguments @('autopilot','enable') -Stage "3. Autopilot (persistent completion)"
        if ($r3.ExitCode -ne 0) { $transcript.Add("ВНИМАНИЕ: autopilot enable вернул $($r3.ExitCode) — рой не будет держать задачи до полного закрытия.") }
    }

    # Queen-сессия. Наш внешний гейт (dry-run -> явное решение человека) уже
    # сыграл роль подтверждения — поэтому дефолт движка (--dangerously-skip-permissions)
    # НЕ отключается: отключить его здесь значило бы, что headless-сессия молча
    # виснет на первом же внутреннем запросе разрешения, которого некому одобрить.
    # --non-interactive обязателен: запуск идёт из скрипта, TTY нет, и без флага
    # сессия может встать на внутреннем интерактивном промпте, которого некому
    # ответить. Проверено 08.08.2026 — с флагом exit 0 и результат возвращается.
    $run = Invoke-Ruflo -Arguments @('hive-mind','spawn','--claude','-o',$fullObjective,'--non-interactive') -Stage '4. Execution (Queen session)'
    $runOut = $run.Output
    $transcript.Add("Проверять реальность роя — по логу (`"name`":`"mcp__claude-flow__`") и по росту Completed в hive-mind status, не по самоотчёту сессии.")

    # ГДЕ ИСКАТЬ МАРКЕРЫ — в выводе ДВИЖКА, а не во всём захваченном потоке.
    #
    # $runOut содержит и транскрипт Queen-сессии: всё, что она читала и писала.
    # Прогон 10.08.2026 (TASK-OBS-0045) на этом и сломался: сессии было поручено
    # править ЭТОТ ЖЕ файл, она его прочитала, строки с маркерами попали в поток —
    # и проверка объявила «тихую деградацию» на собственном исходном коде. Рой
    # отработал (11 вызовов mcp__claude-flow__, 4 агента, файлы изменены), а обвязка
    # записала failed.
    #
    # Разделение простое и надёжное: сессия говорит строками JSON (stream-json),
    # движок — обычным текстом, и деградацию он печатает ДО старта сессии. Поэтому
    # маркеры ищем только в строках, не начинающихся с '{'. Тот же класс, что
    # ERR-2026-000192 у хука-запрета: образец, найденный в ДАННЫХ, принят за факт.
    $engineOut = ($runOut -split "`r?`n" | Where-Object { $_ -notmatch '^\s*\{' }) -join "`n"

    # НЕ верить exit-коду и факту "команда отработала" — движок сам может тихо
    # не запустить Claude Code и просто напечатать инструкции, при этом exit
    # остаётся 0. Проверено 06.08.2026: отчёт писал "EXECUTED" при реально
    # несостоявшемся запуске (см. runOut ниже). Ищем маркер деградации явно.
    if ($engineOut -match 'Claude Code CLI not found' -or $engineOut -match 'Falling back to displaying instructions') {
        $transcript.Add("## Execution FAILED (silent degradation)`nДвижок не нашёл claude.exe и не запустил сессию — рой НЕ РАБОТАЛ, несмотря на то что процесс сам завершился без ошибки. См. вывод выше.")
        Save-Report
        Write-Output "EXECUTION FAILED (Claude Code was not actually launched). Report: $ReportPath"
        exit 7
    }
    # Отдельно — отказ авторизации. Проверено 08.08.2026: истёкшая OAuth-сессия
    # Claude Code роняет ЧЕТВЁРТЫЙ шаг с кодом 1, при этом первые три проходят
    # штатно, и без явного разбора это выглядит как «рой не поехал непонятно почему».
    # ВНИМАНИЕ: здесь ищем во ВСЁМ потоке, а не в $engineOut — и это не небрежность.
    # Два маркера приходят из РАЗНЫХ источников, и область поиска у них разная.
    # Деградацию печатает движок обычным текстом ДО старта сессии; отказ авторизации
    # печатает САМА сессия, а в `--non-interactive` она говорит stream-json — то есть
    # ровно теми строками, которые $engineOut отбрасывает. Первая редакция правки
    # перевела сюда $engineOut заодно и тем убила проверку целиком: истёкший OAuth
    # давал бы общий exit 9 вместо внятного «нужен claude auth login». Найдено codex
    # review 10.08.2026 как blocker.
    #
    # Остаточный риск здесь тот же, что был у деградации: сессия, читающая файл со
    # словами «OAuth session expired», даст ложный отказ. Он принят осознанно —
    # строка редкая и в наших файлах не встречается, а цена пропуска (молчаливый
    # exit 9 на истёкшей авторизации) выше.
    if ($runOut -match 'OAuth session expired' -or $runOut -match 'authentication_failed' -or $runOut -match 'Failed to authenticate') {
        $transcript.Add("## Execution FAILED (authentication)`nУ Claude Code истекла авторизация — Queen-сессия не поднялась. Шаги 1–3 (воркеры, задача в очереди, autopilot) при этом ОТРАБОТАЛИ, состояние роя сохранено. Лечится в обычном PowerShell: claude auth login --claudeai, затем повторить запуск.")
        Save-Report
        Write-Output "EXECUTION FAILED (Claude Code auth expired — run: claude auth login --claudeai). Report: $ReportPath"
        exit 8
    }
    if ($run.ExitCode -ne 0) {
        $transcript.Add("## Execution FAILED`nQueen-сессия завершилась с кодом $($run.ExitCode). Полный вывод шага — выше, причина ищется там, а не повторным прогоном руками.")
        Save-Report
        Write-Output "EXECUTION FAILED (exit $($run.ExitCode)). Report: $ReportPath"
        exit 9
    }

    # --- Приёмка: был ли это рой или одна сессия (ERR-2026-000194, -000195) ---
    # Единственный годный критерий — ФАКТИЧЕСКИЕ вызовы в логе. Счётчики CLI
    # (agent metrics, swarm status, hive-mind status) многоагентность через MCP
    # НЕ отражают: 08.08.2026 по ним был сделан ложный вывод «продукт не умеет
    # исполнять задачи агентами», опровергнутый ЛПР по собственному опыту.
    # Отличать ВЫЗОВ от перечня доступных инструментов: в логе вызов выглядит
    # как "name":"mcp__claude-flow__<tool>", а простое упоминание имени — нет.
    # Оба префикса: сервер переименован в ruflo 12.08.2026, но старые логи и чужие
    # каталоги ещё несут claude-flow — приёмка не должна зависеть от имени регистрации.
    $calls = [regex]::Matches($runOut, '"name"\s*:\s*"(mcp__(?:ruflo|claude-flow)__[A-Za-z_-]+)"')
    $distinct = @($calls | ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique)
    if ($calls.Count -eq 0) {
        $transcript.Add("## ПРИЁМКА: РОЯ НЕ БЫЛО`nВ логе НИ ОДНОГО вызова mcp__claude-flow__* — " +
            "Queen выполнила работу сама, распределения по воркерам не произошло. " +
            "Предметный результат мог быть получен, но как ПРОГОН РОЯ это несостоявшийся " +
            "запуск (ERR-2026-000194). Смотреть, что мешало делегированию: доступны ли были " +
            "инструменты роя сессии (список в начале лога) и не сузила ли цель задачу до соло-работы.")
        Save-Report
        Write-Output "EXECUTED, НО РОЯ НЕ БЫЛО: 0 вызовов mcp__claude-flow__. Report: $ReportPath"
        exit 11
    }
    $transcript.Add("## ПРИЁМКА: рой работал`nВызовов mcp__claude-flow__: $($calls.Count), " +
        "различных инструментов: $($distinct.Count).`n" + ($distinct -join ', '))

    # --- Доля дорогой модели: строка в отчёт, а не догадка постфактум ------------
    # Решение ЛПР 13.08.2026 вместе с эскалацией: потолок без счётчика — пожелание.
    # Считаем по ФАКТИЧЕСКИМ полям model в логе, а не по тому, что просили в цели.
    $byModel = [ordered]@{}
    foreach ($m in [regex]::Matches($runOut, '"model"\s*:\s*"(claude-[a-z0-9.\-]+)"')) {
        $name = $m.Groups[1].Value
        $byModel[$name] = 1 + $(if ($byModel.Contains($name)) { $byModel[$name] } else { 0 })
    }
    $total = ($byModel.Values | Measure-Object -Sum).Sum
    if ($total) {
        $opus = ($byModel.Keys | Where-Object { $_ -like '*opus*' } | ForEach-Object { $byModel[$_] } | Measure-Object -Sum).Sum
        $share = [math]::Round(100.0 * $opus / $total, 1)
        $rows = ($byModel.Keys | ForEach-Object { "$_ = $($byModel[$_])" }) -join ', '
        $verdict = if ($maxOpus -gt 0 -and $opus -gt $maxOpus) { " ПРЕВЫШЕН ПОТОЛОК ($maxOpus): смотреть, по какой причине эскалировали." } else { '' }
        $transcript.Add("## Доля дорогой модели`nopus: $opus из $total ответов ($share %). Потолок: " +
            $(if ($maxOpus -gt 0) { $maxOpus } else { 'не задан' }) + ".$verdict`nПо моделям: $rows")
    }

    # --- Приёмка вторая: СДВИНУЛИСЬ ЛИ МЕТРИКИ ПОСТАНОВКИ ------------------------
    # Указание ЛПР 13.08.2026. Прогон TASK-OBS-0053 закрылся как `done`: рой работал
    # (39 вызовов), выпустил 1.4.0, 115 тестов зелёные — и НИ ОДИН из десяти порогов
    # постановки не сдвинулся, потому что живых действий рой не делал. Приёмка этого
    # не заметила, очередь пометила задачу готовой и выдала кнопку на следующую.
    #
    # Метрики заводились ради двух вещей сразу: чтобы человек видел движение и чтобы
    # РОЙ САМ понимал, что ещё не сделано. Пока их никто не читает машинно, они
    # остаются украшением постановки.
    #
    # Форма выбрана самая дешёвая: рой кладёт рядом с отчётом metrics-<TaskId>.json
    # со списком {метрика, было, порог, стало, достигнут}. Файла нет — это НЕ «метрик
    # не требовалось», это третий исход «предъявить не смог», и он не сворачивается
    # в успех (тот же контракт, что у скана секретов).
    $metricsFile = $metricsPath
    if (-not (Test-Path -LiteralPath $metricsFile)) {
        $transcript.Add("## МЕТРИКИ ПОСТАНОВКИ: НЕ ПРЕДЪЯВЛЕНЫ`nФайла $metricsFile нет. " +
            "Прогон мог сделать работу, но доказательства сдвига метрик не оставил — " +
            "как ЗАКРЫТИЕ ЗАДАЧИ это несостоявшийся прогон, независимо от кода возврата " +
            "и от числа вызовов роя.")
        Save-Report
        Write-Output "EXECUTED, НО МЕТРИКИ НЕ ПРЕДЪЯВЛЕНЫ: нет $metricsFile. Report: $ReportPath"
        exit 12
    }
    # СВЕЖЕСТЬ ФАЙЛА. Проверка «файл есть» пропускала файл ПРОШЛОГО прогона: имя
    # зависит только от TaskId, а не от запуска, и повторный заход по той же задаче
    # находил вчерашние числа и объявлял их предъявленными. Поймано 13.08.2026 на
    # TASK-OBS-0054-бис — отчёт показал утренние значения при живом дневном прогоне.
    # Старый файл — тот же третий исход «предъявить не смог», а не ноль.
    $metricsWritten = (Get-Item -LiteralPath $metricsFile).LastWriteTimeUtc
    if ($metricsWritten -lt $runStartedAtUtc) {
        $transcript.Add("## МЕТРИКИ ПОСТАНОВКИ: ФАЙЛ ОТ ПРОШЛОГО ПРОГОНА`n$metricsFile " +
            "записан $($metricsWritten.ToString('yyyy-MM-dd HH:mm:ss')) UTC, а этот прогон " +
            "начался $($runStartedAtUtc.ToString('yyyy-MM-dd HH:mm:ss')) UTC. Числа в нём " +
            "относятся к другому запуску и доказательством этого не являются.")
        Save-Report
        Write-Output "EXECUTED, МЕТРИКИ ОТ ПРОШЛОГО ПРОГОНА: $metricsFile. Report: $ReportPath"
        exit 12
    }
    try {
        $met = Get-Content -LiteralPath $metricsFile -Raw -Encoding UTF8 | ConvertFrom-Json
        $rows = @($met.metrics)
        $miss = @($rows | Where-Object { -not $_.reached })
        $lines = $rows | ForEach-Object {
            $mark = if ($_.reached) { 'да' } else { 'НЕТ' }
            "| $($_.name) | $($_.before) | $($_.threshold) | $($_.after) | $mark |"
        }
        $table = "| метрика | было | порог | стало | достигнут |`n|---|---|---|---|---|`n" + ($lines -join "`n")
        if ($miss.Count -gt 0) {
            $transcript.Add("## МЕТРИКИ ПОСТАНОВКИ: ДОСТИГНУТЫ НЕ ВСЕ`nНе достигнуто " +
                "$($miss.Count) из $($rows.Count).`n$table`n`nЗадача не закрыта: " +
                "«сделано» без сдвига метрики сделанным не считается.")
            Save-Report
            Write-Output "EXECUTED, МЕТРИКИ НЕ ДОСТИГНУТЫ: $($miss.Count) из $($rows.Count). Report: $ReportPath"
            exit 12
        }
        $transcript.Add("## МЕТРИКИ ПОСТАНОВКИ: ДОСТИГНУТЫ ВСЕ`n$($rows.Count) из $($rows.Count).`n$table")
    } catch {
        $transcript.Add("## МЕТРИКИ ПОСТАНОВКИ: ПРОЧИТАТЬ НЕ СМОГ`n$($_.Exception.Message) ($metricsFile)")
        Save-Report
        Write-Output "EXECUTED, МЕТРИКИ НЕЧИТАЕМЫ: $metricsFile. Report: $ReportPath"
        exit 12
    }
    # ХВОСТ В РАБОЧЕМ ДЕРЕВЕ. Прогон, оставивший правку незакоммиченной, работу до
    # пользователя не довёл: правка живёт только на этой машине, в origin её нет, и
    # обновление плагина приносит прежний код. Ровно это случилось 13.08.2026 на
    # TASK-OBS-0056 (ERR-2026-000236): 230 строк остались в дереве, версия осталась
    # прежней, ЛПР обновил плагин в маркетплейсе и получил вчерашнюю сборку.
    #
    # Считаем ТОЛЬКО отслеживаемые файлы (--untracked-files=no). Рядом в одном репо
    # работают параллельные сессии, и падать на ЧУЖОМ новом файле — это ложный отказ,
    # который дороже пропущенного хвоста. Неотслеживаемое идёт отдельной строкой как
    # замечание, а не как приговор.
    #
    # $ErrorActionPreference='Stop' превращает ЛЮБУЮ строку git в stderr в исключение —
    # та же ловушка, что описана выше для node (строка ~253). Поэтому здесь тот же приём:
    # на время вызовов ставим 'Continue' и судим по $LASTEXITCODE, а не по факту вывода.
    $isRepo = $false; $dirty = ''; $untracked = ''; $checkFailed = ''
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & git -C $TargetPath rev-parse --git-dir *> $null
        $isRepo = ($LASTEXITCODE -eq 0)
        if ($isRepo) {
            $dirty = (& git -C $TargetPath status --porcelain --untracked-files=no 2>$null | Out-String).Trim()
            if ($LASTEXITCODE -ne 0) { $checkFailed = "git status вернул $LASTEXITCODE" }
            $untracked = (& git -C $TargetPath ls-files --others --exclude-standard 2>$null | Out-String).Trim()
        }
    } catch {
        $checkFailed = $_.Exception.Message
    } finally {
        $ErrorActionPreference = $prevEap
    }
    if ($checkFailed) {
        # Третий исход, тот же контракт, что у скана секретов: непроверенное не
        # сворачивается в «чисто». Прогон не валим, но и чистым не объявляем.
        $transcript.Add("## ХВОСТ В РАБОЧЕМ ДЕРЕВЕ: ПРОВЕРИТЬ НЕ СМОГ`n$checkFailed ($TargetPath)")
        $isRepo = $false
    }
    if ($isRepo) {
        if ($dirty) {
            $transcript.Add("## РАБОТА НЕ ДОВЕДЕНА ДО ВЫПУСКА`nВ $TargetPath остались " +
                "незакоммиченные правки отслеживаемых файлов:`n~~~text`n$dirty`n~~~`n" +
                "Прогон мог сделать работу правильно, но она осталась на этой машине. " +
                "Закрытием задачи это не считается: коммит, версия, origin.")
            Save-Report
            Write-Output "EXECUTED, НО РАБОТА НЕ ЗАКОММИЧЕНА: $TargetPath. Report: $ReportPath"
            exit 13
        }
        if ($untracked) {
            $transcript.Add("## ЗАМЕЧАНИЕ: неотслеживаемые файлы в $TargetPath`n~~~text`n$untracked`n~~~`n" +
                "Отказом не считается — рядом работают параллельные сессии, файл может быть чужим.")
        }
    }

    Save-Report
    Write-Output "EXECUTED. Рой работал: $($calls.Count) вызовов роя, $($distinct.Count) инструментов. Report: $ReportPath"
} catch {
    # Раньше исключение уносило с собой весь транскрипт: отчёт обрывался на
    # последнем успешном шаге, и причину падения приходилось искать вручную.
    $transcript.Add("## ПРЕРВАНО ИСКЛЮЧЕНИЕМ`n~~~text`n$($_.Exception.Message)`n~~~`nСтрока: $($_.InvocationInfo.ScriptLineNumber). Всё, что успело выполниться, — выше.")
    Save-Report
    Write-Output "FAILED: $($_.Exception.Message). Report: $ReportPath"
    exit 1
} finally {
    Pop-Location

    # Сканер секретов — по TargetPath (где реально менялись файлы), не по ProjectRoot.
    $scanner = Join-Path $PSScriptRoot 'scan_secrets.ps1'
    if ((Test-Path -LiteralPath $scanner) -and $Approval) {
        try {
            $scanJson = & $scanner -Path $TargetPath -ReportPath (Join-Path $ProjectRoot "leaks-$PID.json") 2>&1 | Out-String
            $scan = $scanJson | ConvertFrom-Json
            $verdict = switch ($scan.outcome) {
                'clean'  { "СЕКРЕТОВ НЕ НАЙДЕНО ($($scan.seconds) c, $($scan.verifiedBy))" }
                'leaks'  { "НАЙДЕНА УТЕЧКА: находок $($scan.findings). Смотреть по файлу и строке в $($scan.report)" }
                default  { "ПРОВЕРИТЬ НЕ СМОГ: $($scan.reason). Это НЕ «чисто»." }
            }
            $transcript.Add("## Secret scan`n$verdict`n~~~json`n$scanJson~~~")
            Save-Report
            Write-Output "SECRET SCAN: $verdict"
        } catch {
            $transcript.Add("## Secret scan`nПРОВЕРИТЬ НЕ СМОГ: $($_.Exception.Message). Это НЕ «чисто».")
            Save-Report
            Write-Output "SECRET SCAN: ПРОВЕРИТЬ НЕ СМОГ — $($_.Exception.Message)"
        }
    }

    # МЕТРИКИ — В ОТЧЁТ, А НЕ ПО ЗАПРОСУ. Указание ЛПР 11.08.2026, второе по счёту:
    # «без метрик не работаем». Инструмент существовал и раньше, но звать его надо было
    # руками — и после прогона этого не делал никто: цифры были, их не видел никто.
    # Здесь тот же класс, что с правилом про ветку: пока проверку зовут вручную, её
    # рано или поздно не позовут.
    $metrics = Join-Path $PSScriptRoot 'ruflo_metrics.py'
    if ((Test-Path -LiteralPath $metrics) -and $Approval) {
        try {
            $m = & python $metrics $ReportPath 2>&1 | Out-String
            $transcript.Add("## Метрики прогона`n~~~text`n$($m.Trim())`n~~~")
            Save-Report
            Write-Output "МЕТРИКИ:`n$($m.Trim())"
        } catch {
            # Отсутствие метрик НЕ выдаётся за ноль: непосчитанное не равно бесплатному.
            $transcript.Add("## Метрики прогона`nПОСЧИТАТЬ НЕ СМОГ: $($_.Exception.Message)")
            Save-Report
        }
    }

    # СТОИМОСТЬ В ДОЛЛАРАХ — ПЛАГИНОМ УЧЁТА, А НЕ НАШИМ СЧЁТОМ. `ruflo-cost-tracker`
    # читает записи сессий Claude Code и раскладывает расход по моделям и ярусам; наш
    # `ruflo_metrics.py` считает по отчёту прогона. Две независимые меры одного — это
    # намеренно: расхождение между ними само по себе находка.
    #
    # Путь к плагину ищется по свежести версии, не константой: плагин обновляется, а
    # версия в пути ломала бы вызов на каждом бампе.
    if ($Approval) {
        try {
            $trackerRoot = Get-ChildItem (Join-Path $env:USERPROFILE '.claude\plugins\cache\ruflo\ruflo-cost-tracker') `
                -Directory -ErrorAction Stop | Sort-Object LastWriteTime -Descending | Select-Object -First 1
            $track = Join-Path $trackerRoot.FullName 'scripts\track.mjs'
            if (Test-Path -LiteralPath $track) {
                Push-Location $ProjectRoot   # снимаем расход СЕССИЙ РОЯ, а не вызывающей сессии
                $cost = & node $track 2>&1 | Out-String
                Pop-Location
                $transcript.Add("## Стоимость по учёту плагина`n~~~text`n$($cost.Trim())`n~~~")
                Save-Report
                Write-Output "СТОИМОСТЬ: см. раздел отчёта «Стоимость по учёту плагина»"
            }
        } catch {
            $transcript.Add("## Стоимость по учёту плагина`nСНЯТЬ НЕ СМОГ: $($_.Exception.Message). Это НЕ «бесплатно».")
            Save-Report
        }
    }

    # Снятие замка БЕЗ -Force. С ним правило «снимает только держатель» было
    # недостижимо: любой прогон сносил чужой живой замок, и замок переставал что-либо
    # значить. Здесь это и не нужно — claim выше сделан ИЗ ЭТОГО ЖЕ процесса
    # (`& $hiveSingle` выполняется в текущем процессе, $PID тот же), поэтому свой
    # замок снимается штатной проверкой pid + время старта.
    if (Test-Path -LiteralPath $hiveSingle) {
        & $hiveSingle -Action release -Root $LockRoot 2>$null | Out-Null
    }

    # Последняя страховка: если ни одна ветка выше отчёт не записала (падение до
    # try, отказ ещё на проверках, что угодно) — пишем здесь. Пустой отчёт хуже
    # неудобного: сегодня из-за потерянного транскрипта причину искали руками.
    if (-not $script:reportSaved -and $transcript.Count -gt 0) {
        $transcript.Add("## Примечание`nОтчёт записан страховочной веткой в finally — основной путь записи не отработал.")
        Save-Report
    }
}
