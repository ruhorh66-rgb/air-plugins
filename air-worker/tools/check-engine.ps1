<#
.SYNOPSIS
  Проверка судьи: двигатель ДОСТИЖИМ, потолки объявлены и осмысленны, задача планировщика
  ОС для запуска НЕ требуется.

.DESCRIPTION
  Шаг 6 плана (AIR-CHG-2026-000091): «Один запуск крутит до вердикта судьи либо до
  объявленного потолка, без задачи планировщика ОС». Решение ЛПР 12.09.2026: свою реализацию
  не заводить — этой роли уже соответствует `woody.ps1` (air-woody), проверено ПРОГОНОМ
  13.09.2026 (`-WhatIfRun` на этом же продукте), а не только чтением кода. Здесь — не
  повтор того прогона, а проверка СВЯЗИ: что goal.json называет команду двигателя, что она
  резолвится, что потолки кампании и одного прогона осмысленны, и что ни сама команда, ни
  вызываемый ею скрипт не заводят задачу планировщика Windows.

  Три группы проверок, дословно из задания:
    1. команда запуска двигателя резолвится (goal.json → engine, путь -File существует,
       исполняемый файл команды резолвится в PATH);
    2. потолки прочитаны и осмысленны — не ноль, не отрицательные: потолки КАМПАНИИ
       (goal.json → maxIterations/maxMinutes/maxRunsPerDay) и потолки ОДНОГО прогона
       (run-config.json → budget.iterations/usd/turns_per_iteration, budget.stall_runs);
    3. запуск не требует задачи планировщика ОС — ни в команде двигателя, ни в самом
       вызываемом скрипте нет Register-ScheduledTask/schtasks; цикл крутится ВНУТРИ
       одного процесса (собственный `while`, несколько вызовов судьи за один запуск).

  Коды: 0 — двигатель достижим и потолки в порядке; 1 — есть нарушение; 2 — проверять нечем.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Continue'

$productRoot = Split-Path -Parent $PSScriptRoot
$goalPath = Join-Path $productRoot 'goal\goal.json'
$configPath = Join-Path $productRoot 'run-config.json'

if (-not (Test-Path -LiteralPath $goalPath -PathType Leaf)) {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: нет $goalPath"
    exit 2
}
if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: нет $configPath"
    exit 2
}

$goal = $null
try { $goal = Get-Content -LiteralPath $goalPath -Raw -Encoding UTF8 | ConvertFrom-Json } catch {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: goal.json не разбирается — $($_.Exception.Message)"
    exit 2
}
$cfg = $null
try { $cfg = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json } catch {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: run-config.json не разбирается — $($_.Exception.Message)"
    exit 2
}

$passed = 0
$failed = 0
function Assert-That([string]$title, [scriptblock]$check) {
    try {
        if (& $check) { Write-Output "[PASS] $title"; $script:passed++ }
        else { Write-Output "[FAIL] $title"; $script:failed++ }
    } catch {
        Write-Output "[FAIL] $title -- $($_.Exception.Message)"
        $script:failed++
    }
}

# --- охват: список того, что вообще есть чем проверять, собран ДО цикла --------------
# Пустой список — это «нечем проверить» (код 2), а не зелёный вердикт по умолчанию.
# Собираем отдельно: пути (команда двигателя, вызываемый скрипт) и потолки (кампании и
# одного прогона) — каждая группа входит в общий список охвата.
$coverage = New-Object System.Collections.Generic.List[object]

if ($goal.engine) { $coverage.Add([pscustomobject]@{ Kind = 'path'; Name = 'engine (goal.json)'; Value = [string]$goal.engine }) }

$ceilings = @(
    @{ Name = 'goal.maxIterations';               Value = $goal.maxIterations }
    @{ Name = 'goal.maxMinutes';                   Value = $goal.maxMinutes }
    @{ Name = 'goal.maxRunsPerDay';                Value = $goal.maxRunsPerDay }
    @{ Name = 'run-config.budget.iterations';      Value = $(if ($cfg.budget) { $cfg.budget.iterations } else { $null }) }
    @{ Name = 'run-config.budget.usd';             Value = $(if ($cfg.budget) { $cfg.budget.usd } else { $null }) }
    @{ Name = 'run-config.budget.turns_per_iteration'; Value = $(if ($cfg.budget) { $cfg.budget.turns_per_iteration } else { $null }) }
)
foreach ($c in $ceilings) {
    if ($null -ne $c.Value) { $coverage.Add([pscustomobject]@{ Kind = 'ceiling'; Name = $c.Name; Value = $c.Value }) }
}
# budget.stall_runs — четвёртый потолок (застой), но у него есть законное умолчание (3) в
# самом woody.ps1: отсутствие поля — не дефект конфигурации, поэтому он не обязателен для
# охвата, но проверяется наравне с прочими, если объявлен.
if ($cfg.budget -and $null -ne $cfg.budget.stall_runs) {
    $coverage.Add([pscustomobject]@{ Kind = 'ceiling'; Name = 'run-config.budget.stall_runs'; Value = $cfg.budget.stall_runs })
}

if ($coverage.Count -eq 0) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: из goal.json и run-config.json не собрано ни одной команды двигателя и ни одного потолка.'
    exit 2
}
Write-Output ("  охват: {0} пунктов ({1} команда(ы), {2} потолок(ов))" -f `
    $coverage.Count, @($coverage | Where-Object { $_.Kind -eq 'path' }).Count, @($coverage | Where-Object { $_.Kind -eq 'ceiling' }).Count)

# --- 1. команда запуска двигателя резолвится ------------------------------------------
Assert-That 'goal.json называет команду двигателя (поле engine)' {
    -not [string]::IsNullOrWhiteSpace([string]$goal.engine)
}

$engineText = [string]$goal.engine

# ФОРМА ВЫЗОВА НЕ ОДНА, И ПРОВЕРКА ОБЯЗАНА ЗНАТЬ ОБЕ. Прежде отсюда доставали только
# аргумент -File, и это работало ровно пока двигателем был скрипт PowerShell. 13.09.2026
# двигателем стал бинарник продукта, договор стал верным — а проверка объявила его
# неполным: «из команды двигателя извлекается путь к скрипту (-File)». Проверка, знающая
# одну форму, проверяет форму, а не предмет. Четвёртый случай за сутки, когда неправа
# оказалась проверка, а не проверяемое.
#
# ПУТИ ОТНОСИТЕЛЬНЫЕ — от корня продукта. Найдено AIR-ENV-002 при развёртывании на второй
# машине: абсолютные пути машины автора делали цель продукта пригодной для одного хоста.
function Resolve-Declared([string]$path) {
    if (-not $path) { return $null }
    if ([System.IO.Path]::IsPathRooted($path)) { return $path }
    return [System.IO.Path]::GetFullPath((Join-Path $productRoot $path))
}

$m = [regex]::Match($engineText, '-File\s+"([^"]+)"')
if (-not $m.Success) { $m = [regex]::Match($engineText, "-File\s+'([^']+)'") }
$exeToken = ($engineText.Trim() -split '\s+')[0].Trim('"', "'")
$enginePath = if ($m.Success) { Resolve-Declared $m.Groups[1].Value } else { Resolve-Declared $exeToken }

Assert-That 'из команды двигателя извлекается исполняемый файл' { $null -ne $enginePath }.GetNewClosure()
Assert-That 'двигатель существует на этой машине' {
    if (-not $enginePath) { throw 'путь не извлечён — нечем проверить существование' }
    Test-Path -LiteralPath $enginePath -PathType Leaf
}.GetNewClosure()

# Исполняемый файл обязан резолвиться: либо как путь внутри продукта, либо в PATH этого
# процесса. Второе — для форм вызова через powershell/интерпретатор.
# Значение вычисляется ДО замыкания. .GetNewClosure() кладёт блок в собственный модуль,
# и функции скрипта внутри него не видны — вызов Resolve-Declared оттуда падал с «не
# распознано», а проверка объявляла исправный договор сломанным. Внутрь замыкания уходят
# только переменные.
$exeResolvesAsPath = Test-Path -LiteralPath (Resolve-Declared $exeToken) -PathType Leaf
$exeResolvesInPath = [bool](Get-Command $exeToken -ErrorAction SilentlyContinue)
Assert-That "исполняемый файл команды резолвится: $exeToken" {
    $exeResolvesAsPath -or $exeResolvesInPath
}.GetNewClosure()

# --- 2. потолки прочитаны и осмысленны: не ноль, не отрицательные ----------------------
foreach ($c in $ceilings) {
    if ($null -eq $c.Value) { continue }
    Assert-That ("потолок положителен: {0} = {1}" -f $c.Name, $c.Value) {
        $v = 0.0
        [double]::TryParse([string]$c.Value, [ref]$v) -and ($v -gt 0)
    }.GetNewClosure()
}
if ($cfg.budget -and $null -ne $cfg.budget.stall_runs) {
    Assert-That ("потолок положителен: run-config.budget.stall_runs = {0}" -f $cfg.budget.stall_runs) {
        $v = 0.0
        [double]::TryParse([string]$cfg.budget.stall_runs, [ref]$v) -and ($v -gt 0)
    }
} else {
    Write-Output '  run-config.budget.stall_runs не объявлен — законно, woody.ps1 берёт умолчание 3.'
}

# --- 3. запуск не требует задачи планировщика ОС ---------------------------------------
Assert-That 'команда двигателя не заводит задачу планировщика (schtasks/Register-ScheduledTask)' {
    $engineText -notmatch '(?i)schtasks|Register-ScheduledTask'
}

# ИСТОЧНИК ДВИГАТЕЛЯ ИЩЕТСЯ ПО ТОМУ, ЧЕМ ОН ЯВЛЯЕТСЯ, А НЕ ПО ТОМУ, ЧЕМ БЫЛ.
#
# Прежде здесь читался текст .ps1 и искались `while` и `Invoke-Judge`. 13.09.2026
# двигателем стал бинарник: договор верен, механизм исправен — а проверка объявила его
# сломанным, потому что искала синтаксис PowerShell в исполняемом файле. Это уже четвёртый
# за сутки случай, когда неправа оказалась ПРОВЕРКА: она знала одну форму предмета и
# проверяла форму вместо предмета.
#
# Предмет же неизменен и не зависит от языка: цикл крутится ВНУТРИ одного процесса и зовёт
# судью больше одного раза за запуск, а задачи планировщика ОС не заводит ни он, ни команда
# его запуска.
$engineSrcPath = $null
$engineLang = $null
if ($enginePath -and (Test-Path -LiteralPath $enginePath -PathType Leaf)) {
    if ($enginePath -like '*.ps1') {
        $engineSrcPath = $enginePath; $engineLang = 'powershell'
    } elseif ($enginePath -like '*.exe') {
        # У собранного файла исходник рядом, в самом продукте: cmd\loop.go.
        $candidate = Join-Path $productRoot 'cmd\loop.go'
        if (Test-Path -LiteralPath $candidate -PathType Leaf) { $engineSrcPath = $candidate; $engineLang = 'go' }
    }
}

if (-not $engineSrcPath) {
    Write-Output '  ИСХОДНИК ДВИГАТЕЛЯ НЕ НАЙДЕН — свойства цикла не проверены. Это «нечем проверить», и оно названо, а не пропущено молча.'
} else {
    $engineSrc = [System.IO.File]::ReadAllText($engineSrcPath, [System.Text.Encoding]::UTF8)
    Assert-That "сам двигатель не заводит задачу планировщика ОС ($engineLang)" {
        $engineSrc -notmatch '(?i)Register-ScheduledTask|schtasks\.exe|schtasks '
    }.GetNewClosure()
    # Положительное доказательство, а не только отсутствие отрицательного: без него
    # «без задачи планировщика» было бы верно и для программы, которая просто ничего
    # не повторяет.
    $loopPattern = if ($engineLang -eq 'go') { '(?m)^\s*for\s*\{' } else { '(?m)^\s*while\s*\(' }
    Assert-That "двигатель крутится в собственном цикле внутри одного процесса ($engineLang)" {
        $engineSrc -match $loopPattern
    }.GetNewClosure()
    $judgePattern = if ($engineLang -eq 'go') { '\bc\.judge\(\)' } else { 'Invoke-Judge\b' }
    $judgeCalls = @([regex]::Matches($engineSrc, $judgePattern))
    Assert-That 'двигатель зовёт судью больше одного раза за один запуск (петля, а не разовый вызов)' {
        $judgeCalls.Count -ge 2
    }.GetNewClosure()
}

Write-Output ''
Write-Output ("прошло {0}, провалено {1}" -f $passed, $failed)
if ($failed -gt 0) { exit 1 }
exit 0
