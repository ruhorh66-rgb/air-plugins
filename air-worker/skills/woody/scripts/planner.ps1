#requires -Version 5.1
<#
.SYNOPSIS
    Планировщик: разбивка цели на шаги с назначением ступеней ОДНИМ дорогим вызовом.

.DESCRIPTION
    Пятая роль, названная ЛПР сверх четырёх ролей AUTO-080 (цель, судья, исполнитель,
    двигатель) и прямо отделённая им от планировщика ОС: «не нужен тебе планировщик
    вообще от слова совсем» — это про задачу Windows, а не про разбивку цели.

    ЗАЧЕМ ОН ЭКОНОМИЧЕСКИ. Разбивка — единственное место, где суждение окупается.
    Ошибка здесь тиражируется на все последующие прогоны: шаг, назначенный дорогой
    модели по недосмотру, платится столько раз, сколько раз петля его возьмёт. Поэтому
    один дорогой вызов на разбивку — и дальше механическое исполняется скриптами.
    Обратный порядок — разведка боем на каждом шаге — выглядит работой и стоит вдесятеро.

    ПОЧЕМУ ОН ПРЕДЛАГАЕТ, А НЕ ПИШЕТ. План правится руками — та же мысль, что
    *.user.rules у AIR Kill Switch: машина исполняет, человек владеет разбивкой.
    Планировщик, молча переписывающий PLAN.md, отнимает у человека единственное место,
    где тот решает. Поэтому вывод ложится в PLAN.proposed.md, и перенос делает человек.
    Ключ -Apply пишет PLAN.md только когда его НЕТ вовсе: у пустого места владельца ещё
    нет, а у написанного — есть.

    ОДИН ВЫЗОВ ЗНАЧИТ ОДИН. Не «немного», не «сколько понадобится». Счёт вызовов
    печатается в замере и проверяется check-planner.ps1: разбивка, требующая пяти
    заходов, — это разведка боем, от которой шаг и защищает.

.PARAMETER ProductRoot
    Корень продукта: goal/goal.json, run-config.json, PLAN.md.

.PARAMETER Apply
    Записать PLAN.md, если его нет. Существующий план не трогается никогда.

.PARAMETER Model
    Модель разбивки. Умолчание — верх лестницы продукта, потому что это и есть то
    единственное место, ради которого верх лестницы держат.

.PARAMETER DryRun
    Собрать задание и показать его, не звать модель. Бесплатно.
#>
[CmdletBinding()]
param(
    [string]$ProductRoot = (Get-Location).Path,
    [switch]$Apply,
    [string]$Model,
    [switch]$DryRun,
    # Разобрать УЖЕ ПОЛУЧЕННЫЙ ответ, не платя снова. Заведено 13.09.2026, когда ошибка
    # в моей же проверке уронила разбор ПОСЛЕ состоявшегося вызова ценой почти доллара.
    # Дефект проверки не должен стоить второго дорогого вызова — это ровно та
    # расточительность, от которой шаг и защищает.
    [string]$UseAnswer
)

$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

# Резолв ступени — общая функция со стражем и петлёй. Своя копия разошлась бы на первой правке.
. (Join-Path (Split-Path -Parent $PSScriptRoot) 'lib\ladder.ps1')

function Write-Line([string]$t) { Write-Output ((Get-Date).ToString('HH:mm:ss') + '  ' + $t) }

if (-not (Test-Path -LiteralPath $ProductRoot -PathType Container)) {
    Write-Line "ОТКАЗ: нет каталога продукта — $ProductRoot"; exit 2
}
$ProductRoot = (Resolve-Path -LiteralPath $ProductRoot).Path

$cfgPath = Join-Path $ProductRoot 'run-config.json'
if (-not (Test-Path -LiteralPath $cfgPath)) {
    Write-Line "ОТКАЗ: нет run-config.json. Разбивка без лестницы назначала бы ступени наугад."; exit 2
}
$cfg = Get-Content -LiteralPath $cfgPath -Raw -Encoding UTF8 | ConvertFrom-Json
$ladder = @($cfg.ladder)
if (-not $ladder.Count) { Write-Line 'ОТКАЗ: лестница не объявлена в run-config.json'; exit 2 }

# --- цель. Без неё разбивать нечего, и выдумывать её планировщик не станет ----
$goalPath = Join-Path $ProductRoot 'goal\goal.json'
if (-not (Test-Path -LiteralPath $goalPath)) {
    Write-Line "ОТКАЗ: нет $goalPath. Цель как файл — признак 1 постановки; разбивать пересказ из промпта запрещено."
    exit 2
}
$goal = Get-Content -LiteralPath $goalPath -Raw -Encoding UTF8 | ConvertFrom-Json

# Постановка — источник признаков достижения. Отсутствие не отказ: продукт может вестись
# одной целью-файлом. Но это НАЗЫВАЕТСЯ, а не умалчивается: разбивка без признаков
# достижения слабее, и человек должен знать, что получил именно её.
$objText = ''
$objNote = 'постановка не объявлена в goal.json — разбивка идёт по одной цели-файлу'
$objRef = if ($goal.objective) { [string]$goal.objective } else { '' }
if ($objRef) {
    $objFull = if (Test-Path -LiteralPath $objRef) { $objRef } else { Join-Path $ProductRoot $objRef }
    if (Test-Path -LiteralPath $objFull) {
        $objText = Get-Content -LiteralPath $objFull -Raw -Encoding UTF8
        $objNote = "постановка прочитана: $objFull"
    } else {
        $objNote = "постановка объявлена ($objRef), но файла нет — разбивка идёт без признаков достижения"
    }
}

if (-not $Model) { $Model = $ladder[-1] }
$modelName = ($Model -split ':')[0]

# --- задание разбивщику ------------------------------------------------------
# Правило назначения ступени дано ЯВНО и с основанием: без него модель назначает по
# ожидаемой трудности, а надо — по природе шага. Это тот самый дефект, ради которого
# скил вообще написан: «модель на ступени script — дефект, а не выбор».
$prompt = @"
Ты размечаешь работу для петли Дятла Вуди. Тебя зовут ОДИН раз: разбивка делается
дорогим вызовом, чтобы дальше механическое исполнялось скриптами, а не моделью.

ЦЕЛЬ ПРОДУКТА
$($goal.goal)

ОПИСАНИЕ ЦЕЛИ (goal.json)
$($goal | ConvertTo-Json -Depth 6)

ПОСТАНОВКА ($objNote)
$objText

ЛЕСТНИЦА ПРОДУКТА (других ступеней не существует)
$($ladder -join ', ')

КАК НАЗНАЧАТЬ СТУПЕНЬ — ПО ПРИРОДЕ ШАГА, А НЕ ПО ОЖИДАЕМОЙ ТРУДНОСТИ:
- детерминированное с проверяемым точным выходом -> script. Модель здесь ДЕФЕКТ:
  переименования, правка путей, генерация манифеста, сверка хэшей, счётчик, замена по
  образцу. Отдать такое модели значит заплатить за бесплатное.
- механическое, но текстовой формы -> haiku: применить известный образец ко многим
  файлам, заполнить шаблон, разложить список в структуру.
- обычный кодинг с названной причиной -> sonnet: функция, тест, починка.
- только там, где нужно СУЖДЕНИЕ -> opus: архитектура, неоднозначный отказ,
  противоречащие свидетельства.
- шаг, который закрывает не сессия, а человек (выпуск, покупка, доступ, перезагрузка,
  вход в учётную запись вендора), ступени не имеет: ставь тире и слово «гейт: ЛПР».

ПОРЯДОК ШАГОВ НЕ ПРОИЗВОЛЕН: то, что проверяется чем-то, идёт ПОСЛЕ того, чем
проверяется. Судья раньше тех, кого он судит.

ОТВЕТ — ТОЛЬКО таблица Markdown, ровно четыре колонки, без текста до и после.
Лишняя колонка ломает разбор молча: план даёт ноль шагов, и петля отвечает «план пуст»
на живом плане. Закрытых шагов не помечай — всё предстоит.

| № | Шаг | Ступень | Судья |
|---|-----|---------|-------|
| 1 | что сделать, одной строкой, проверяемо | ``script`` | исполнитель: ведущая |
| 2 | следующее | ``sonnet`` | исполнитель: субагент |
| 3 | выпуск версии | — | гейт: ЛПР |
"@

$outDir = Join-Path $ProductRoot '.woody'
if (-not (Test-Path -LiteralPath $outDir)) { New-Item -ItemType Directory -Path $outDir -Force | Out-Null }
$promptPath = Join-Path $outDir 'planner-prompt.txt'
[IO.File]::WriteAllText($promptPath, $prompt, (New-Object Text.UTF8Encoding $false))

Write-Line "продукт   : $ProductRoot"
Write-Line "цель      : $($goal.goal)"
Write-Line "разбивка  : модель $Model (верх лестницы), ОДИН вызов"
Write-Line "$objNote"

if ($DryRun) {
    Write-Line 'сухой прогон: задание собрано, модель не зовётся'
    Write-Output ''
    Write-Output $prompt
    exit 0
}

# --- один вызов --------------------------------------------------------------
# Инструмент ищется по ответу, а не по прибитому пути: тот же класс отказа, что оборвал
# верх лестницы air-worker 12.09.2026 — npm-путь остался в коде, а клиент переехал.
$claudeExe = $null
foreach ($c in @(
    $env:CLAUDE_JUDGE_EXE,
    (Get-Command claude -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty Source),
    (Join-Path $env:USERPROFILE '.local\bin\claude.exe')
)) { if ($c -and (Test-Path -LiteralPath $c)) { $claudeExe = $c; break } }
if (-not $claudeExe) {
    Write-Line 'ОТКАЗ: claude не найден. Это «нечем исполнить», а не «модель не справилась».'
    exit 2
}

# ТОКЕН МОГ НЕ ДОЕХАТЬ. Найдено живым прогоном 13.09.2026: claude отвечает «Not logged
# in», хотя переменная задана на уровне пользователя — процесс оболочки стартовал ДО
# того, как её поставили, и своего окружения не перечитывает. Тот же класс уже чинен в
# claude_judge_run.py продукта air-worker; здесь его не было, и разбивка падала сразу.
# Различать обязательно: «нечем исполнить» и «модель не справилась» — разные исходы.
# ЗНАЧЕНИЕ НЕ ПЕЧАТАЕТСЯ И НЕ ЛОЖИТСЯ В ЖУРНАЛ — только «взят» или «не найден».
if (-not $env:CLAUDE_CODE_OAUTH_TOKEN) {
    try {
        $userTok = [Environment]::GetEnvironmentVariable('CLAUDE_CODE_OAUTH_TOKEN', 'User')
        if ($userTok) {
            $env:CLAUDE_CODE_OAUTH_TOKEN = $userTok
            Write-Line 'токен взят из окружения пользователя (в процессе его не было)'
        }
    } catch { }
}
$outPath = Join-Path $outDir 'planner-answer.json'
$errPath = Join-Path $outDir 'planner-answer.err'
if ($UseAnswer) { Copy-Item -LiteralPath $UseAnswer -Destination $outPath -Force; Write-Line 'разбор сохранённого ответа, вызова нет' }
else {
Write-Line 'зову разбивщика (один вызов)...'
$psi = Start-Process -FilePath $claudeExe -PassThru -Wait -WindowStyle Hidden `
       -ArgumentList @('-p', '--model', $modelName, '--output-format', 'json') `
       -RedirectStandardInput $promptPath -RedirectStandardOutput $outPath -RedirectStandardError $errPath
if ($psi.ExitCode -ne 0) {
    Write-Line "ОТКАЗ: разбивщик вернул код $($psi.ExitCode)"
    try { Get-Content -LiteralPath $errPath -Raw | Write-Output } catch { }
    exit 1
}
}

$ans = Get-Content -LiteralPath $outPath -Raw -Encoding UTF8 | ConvertFrom-Json
$table = [string]$ans.result
$cost = $ans.total_cost_usd
$turns = $ans.num_turns

# --- проверка ответа ПЕРЕД записью -------------------------------------------
# Разбивщик мог ответить прозой, пятью колонками или пустотой. Записать такое значит
# отдать человеку заготовку под видом плана.
$rows = @()
foreach ($line in ($table -split "`r?`n")) {
    if ($line -match '^\s*\|\s*(\d+)\s*\|') { $rows += $line }
}
$cols = 0
if ($rows.Count) { $cols = (($rows[0].Trim().Trim('|')) -split '\|').Count }

$costText = if ($null -eq $cost) { 'не измерено' } else { '$' + ('{0:N4}' -f [double]$cost) }
$turnsText = if ($null -eq $turns) { 'не измерено' } else { [string][int]$turns }
Write-Output ("Замер: разбивка · вызовов 1 · {0} · ходов {1}" -f $costText, $turnsText)

if (-not $rows.Count) {
    Write-Line 'ОТКАЗ: в ответе нет ни одной строки шага — это не разбивка.'
    exit 1
}
if ($cols -ne 4) {
    Write-Line "ОТКАЗ: в таблице $cols колонок вместо четырёх. Такой план петля разберёт в НОЛЬ шагов и скажет «план пуст» — молча."
    exit 1
}
$bad = @()
foreach ($r in $rows) {
    $parts = ($r.Trim().Trim('|')) -split '\|'
    $tier = $parts[2].Trim().Trim('`').Trim()
    if ($tier -eq '—' -or $tier -eq '-' -or $r -match 'гейт') { continue }
    if (-not (Resolve-LadderTier -Tier $tier -Ladder $ladder).Found) { $bad += $tier }
}
if ($bad.Count) {
    Write-Line ("ОТКАЗ: назначены ступени вне лестницы продукта — " + (($bad | Select-Object -Unique) -join ', '))
    exit 1
}

$header = @"
# PLAN — предложен Планировщиком $((Get-Date).ToString('dd.MM.yyyy HH:mm'))

Постановка: $objRef
Разбивка сделана ОДНИМ вызовом модели $Model. Замер: $costText, ходов $turnsText.

ЭТО ПРЕДЛОЖЕНИЕ, А НЕ ПЛАН. План правится руками: машина исполняет, человек владеет
разбивкой. Перенеси в PLAN.md то, с чем согласен, — целиком или частями.

"@
$proposed = Join-Path $ProductRoot 'PLAN.proposed.md'
[IO.File]::WriteAllText($proposed, ($header + $table + "`r`n"), (New-Object Text.UTF8Encoding $true))
Write-Line "шагов предложено: $($rows.Count); записано: $proposed"

$planPath = Join-Path $ProductRoot 'PLAN.md'
if ($Apply) {
    if (Test-Path -LiteralPath $planPath) {
        Write-Line 'PLAN.md существует — НЕ ТРОГАЮ. У написанного плана есть владелец, и это не я.'
    } else {
        [IO.File]::WriteAllText($planPath, ($header + $table + "`r`n"), (New-Object Text.UTF8Encoding $true))
        Write-Line "PLAN.md создан (его не было): $planPath"
    }
}

Write-Output ''
Write-Output 'Дальше: перенести согласованные шаги из PLAN.proposed.md в PLAN.md'
Write-Output 'От тебя жду: сверить разбивку и ступени — это единственное место, где решение твоё'
exit 0
