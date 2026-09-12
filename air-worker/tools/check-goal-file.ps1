<#
.SYNOPSIS
  Проверка судьи: цель существует файлом, договор описания цели полон и не дублирует судью.

.DESCRIPTION
  Признак 1 постановки AIR-CHG-2026-000091, роль «цель» нормы AUTO-080: файл задания,
  который исполнитель читает ПЕРВЫМ ДЕЙСТВИЕМ, а не текст в промпте и не устная постановка.
  До этой проверки такого файла у продукта не было: заявка (`apply_verification.py`/
  `ladder.py` claim) собиралась словарём в памяти вызывающего кода и не переживала прогон —
  её нельзя было положить в git, показать человеку до запуска или повторить в новой сессии.

  Проверяется `goal\goal.json` — что взято из образца нормы и что осмысленно заменено,
  объяснено в самом файле его же полями `_...`, эта проверка их не пересказывает.

  Проверяется:
    1. файл существует и разбирается;
    2. объявлены все поля контракта, и они того типа, что нужен;
    3. цель НЕ дублирует судью — в ней нет ни `checks`, ни `checklist`: они живут
       единственно в run-config.json (одно состояние — один источник);
    4. в файле нет полей вне договора — новое поле должно быть НАЗВАНО отказом этой
       проверки, а не молча пропущено;
    5. `taskFile` — путь, а не содержимое: не может физически уехать в аргумент
       командной строки, потому что в схеме файла нет поля, куда положить текст задания
       целиком;
    6. объявленные пути (workdir, taskFile, судья) существуют на этой машине;
    7. вызывающий код (air-woody `Invoke-ModelStep`, которым продукт объявил себя
       управляемым через `run-config.json → orchestration`) на деле пишет задание файлом
       и передаёт исполнителю короткую директиву с путём, а не собранное содержимое —
       тот же признак, что требование 3 нормы уже доказало на уровне вызова модели
       (`check-ladder-reachable.ps1` → `input=prompt`), здесь — на уровне продукта.

  Коды: 0 — договор цели в порядке; 1 — есть нарушение; 2 — проверять нечем.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Continue'

$productRoot = Split-Path -Parent $PSScriptRoot
$goalPath = Join-Path $productRoot 'goal\goal.json'

if (-not (Test-Path -LiteralPath $goalPath -PathType Leaf)) {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: нет $goalPath"
    exit 2
}

$goal = $null
try { $goal = Get-Content -LiteralPath $goalPath -Raw -Encoding UTF8 | ConvertFrom-Json } catch {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: goal.json не разбирается — $($_.Exception.Message)"
    exit 2
}

$passed = 0
$failed = 0
function Assert-That([string]$title, [scriptblock]$check) {
    try {
        if (& $check) { Write-Output "  [PASS]  $title"; $script:passed++ }
        else { Write-Output "  [FAIL]  $title"; $script:failed++ }
    } catch {
        Write-Output "  [FAIL]  $title -- $($_.Exception.Message)"
        $script:failed++
    }
}

$props = @($goal.PSObject.Properties.Name)

# --- 1. обязательные поля контракта присутствуют и того типа, что нужен --------------
$required = @('enabled', 'workdir', 'taskFile', 'judge', 'maxIterations', 'maxMinutes', 'maxRunsPerDay')
foreach ($f in $required) {
    Assert-That "поле объявлено: $f" { $props -contains $f }.GetNewClosure()
}

Assert-That 'enabled — булево' { $goal.enabled -is [bool] }

foreach ($n in @('maxIterations', 'maxMinutes', 'maxRunsPerDay')) {
    Assert-That "$n — положительное целое" {
        $v = $goal.$n
        ($null -ne $v) -and ([string]$v -match '^\d+$') -and ([int]$v -gt 0)
    }.GetNewClosure()
}

foreach ($n in @('workdir', 'taskFile', 'judge')) {
    Assert-That "$n — непустая строка" { -not [string]::IsNullOrWhiteSpace([string]$goal.$n) }.GetNewClosure()
}

# --- 2. цель не дублирует судью: одно состояние — один источник ----------------------
Assert-That 'goal.json не держит checks — они единственно в run-config.json' { $props -notcontains 'checks' }
Assert-That 'goal.json не держит checklist — он единственно в run-config.json' { $props -notcontains 'checklist' }

# --- 3. нет полей вне договора — новое поле НАЗЫВАЕТСЯ, а не проглатывается молча -----
# Название печатается В ЗАГОЛОВКЕ, а не Write-Output'ом ИЗНУТРИ проверочного блока: любой
# вывод внутри { } уходит в тот же поток, что и булево значение, и `if (& $check)` считает
# истиной уже сам факт непустого МАССИВА результата — неважно, что последний элемент $false.
# Ровно так эта проверка на первом прогоне пропустила подставной "surpriseField" молча —
# то самое, что она обязана НЕ делать. Поймано вставкой поля и прогоном, не чтением кода.
$allowed = $required + @($props | Where-Object { $_.StartsWith('_') })
$unknown = @($props | Where-Object { $allowed -notcontains $_ })
$unknownTitle = if ($unknown.Count -gt 0) {
    "в goal.json нет полей вне договора (найдены: $($unknown -join ', '))"
} else { 'в goal.json нет полей вне договора' }
Assert-That $unknownTitle { $unknown.Count -eq 0 }.GetNewClosure()

# --- 4. taskFile — путь, а не содержимое, физически ------------------------------------
Assert-That 'taskFile похож на путь, а не на вклеенное содержимое цели' {
    $tf = [string]$goal.taskFile
    $tf.Length -gt 0 -and $tf.Length -le 260 -and $tf -notmatch "`n" -and $tf -notmatch "`r"
}

# --- 5. охват: список объявленных путей собран и он НЕ ПУСТ — до цикла, не после ------
$toCheck = New-Object System.Collections.Generic.List[object]
if ($goal.workdir) { $toCheck.Add([pscustomobject]@{ Name = 'workdir'; Path = [string]$goal.workdir }) }
if ($goal.workdir -and $goal.taskFile) {
    $tf = [string]$goal.taskFile
    $tfPath = if ([System.IO.Path]::IsPathRooted($tf)) { $tf } else { Join-Path ([string]$goal.workdir) $tf }
    $toCheck.Add([pscustomobject]@{ Name = 'taskFile'; Path = $tfPath })
}
if ($goal.judge) {
    $judgeText = [string]$goal.judge
    $m = [regex]::Match($judgeText, '-File\s+"([^"]+)"')
    if (-not $m.Success) { $m = [regex]::Match($judgeText, "-File\s+'([^']+)'") }
    if ($m.Success) { $toCheck.Add([pscustomobject]@{ Name = 'судья (-File)'; Path = $m.Groups[1].Value }) }
}
if ($toCheck.Count -eq 0) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: из goal.json не собрано ни одного пути — договор пуст либо не разобран.'
    exit 2
}
foreach ($item in $toCheck) {
    Assert-That ("объявленный путь существует: {0} -> {1}" -f $item.Name, $item.Path) {
        Test-Path -LiteralPath $item.Path
    }.GetNewClosure()
}

# --- 6. вызывающий код: содержимое цели уходит файлом, аргументу достаётся только путь -
# Тот же признак, что check-ladder-reachable.ps1 уже доказал на уровне ВЫЗОВА МОДЕЛИ
# (`input=prompt` в claude_judge_run.py). Здесь — на уровне ПРОДУКТА: run-config.json
# объявил orchestration.enabled=true, а тем самым вызывающим — air-woody Invoke-ModelStep.
# Путь переопределяем переменной среды, а не прибиваем единственным вариантом — по тому
# же принципу, что LLM_QUEUE_DISPATCHER в ladder.py.
$woodyScript = if ($env:AIR_WOODY_SCRIPT) { $env:AIR_WOODY_SCRIPT } else { 'E:\-5-\014_Skills\air-woody\scripts\woody.ps1' }
Assert-That 'вызывающий код (air-woody) пишет задание ФАЙЛОМ, а не строкой в аргумент' {
    if (-not (Test-Path -LiteralPath $woodyScript -PathType Leaf)) {
        throw "air-woody не найден по '$woodyScript' (переопределяется AIR_WOODY_SCRIPT) — нечем свериться"
    }
    $woodyText = [System.IO.File]::ReadAllText($woodyScript, [System.Text.Encoding]::UTF8)
    ($woodyText -match '\$taskFile\s*=\s*Join-Path') -and
        ($woodyText -match '\[System\.IO\.File\]::WriteAllText\(\$taskFile')
}
Assert-That 'исполнителю уходит короткая директива с путём, а не собранное содержимое' {
    if (-not (Test-Path -LiteralPath $woodyScript -PathType Leaf)) {
        throw "air-woody не найден по '$woodyScript' — нечем свериться"
    }
    $woodyText = [System.IO.File]::ReadAllText($woodyScript, [System.Text.Encoding]::UTF8)
    # Обе ветви вызова модели (claude/codex) зовутся от короткой $prompt; ни одна не
    # передаёт $lines — переменную, в которую собрано полное содержимое файла задания.
    ($woodyText -match 'Invoke-Claude\s+\$prompt') -and
        ($woodyText -match 'Invoke-Codex\s+\$prompt') -and
        ($woodyText -notmatch 'Invoke-(Claude|Codex)\s+\$lines')
}

Write-Output ''
Write-Output ("прошло {0}, провалено {1}" -f $passed, $failed)
if ($failed -gt 0) { exit 1 }
exit 0
