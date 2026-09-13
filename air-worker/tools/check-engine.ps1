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
$m = [regex]::Match($engineText, '-File\s+"([^"]+)"')
if (-not $m.Success) { $m = [regex]::Match($engineText, "-File\s+'([^']+)'") }
$enginePath = if ($m.Success) { $m.Groups[1].Value } else { $null }

Assert-That 'из команды двигателя извлекается путь к скрипту (-File)' { $null -ne $enginePath }.GetNewClosure()
Assert-That 'скрипт двигателя существует на этой машине' {
    if (-not $enginePath) { throw 'путь не извлечён — нечем проверить существование' }
    Test-Path -LiteralPath $enginePath -PathType Leaf
}.GetNewClosure()

# Первое слово команды — исполняемый файл (powershell/powershell.exe); он обязан
# резолвиться в PATH этого процесса, а не предполагаться существующим.
$exeToken = ($engineText.Trim() -split '\s+')[0]
Assert-That "исполняемый файл команды резолвится в PATH: $exeToken" {
    [bool](Get-Command $exeToken -ErrorAction SilentlyContinue)
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

if ($enginePath -and (Test-Path -LiteralPath $enginePath -PathType Leaf)) {
    $engineSrc = [System.IO.File]::ReadAllText($enginePath, [System.Text.Encoding]::UTF8)
    Assert-That 'сам скрипт двигателя не заводит задачу планировщика ОС' {
        $engineSrc -notmatch '(?i)Register-ScheduledTask|schtasks\.exe|schtasks '
    }.GetNewClosure()
    # Положительное доказательство, а не только отсутствие отрицательного: цикл крутится
    # ВНУТРИ одного процесса (собственный while) и зовёт судью больше одного раза за
    # запуск — иначе «без задачи планировщика» было бы верно и для скрипта, который просто
    # ничего не повторяет.
    Assert-That 'двигатель крутится в собственном цикле (while) внутри одного процесса' {
        $engineSrc -match '(?m)^\s*while\s*\('
    }.GetNewClosure()
    $judgeCalls = @([regex]::Matches($engineSrc, 'Invoke-Judge\b'))
    Assert-That 'двигатель зовёт судью больше одного раза за один запуск (петля, а не разовый вызов)' {
        $judgeCalls.Count -ge 2
    }.GetNewClosure()
}

Write-Output ''
Write-Output ("прошло {0}, провалено {1}" -f $passed, $failed)
if ($failed -gt 0) { exit 1 }
exit 0
