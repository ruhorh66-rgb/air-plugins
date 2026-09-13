#requires -Version 5.1
<#
    Проверка: ГРАНИЦА МЕЖДУ GO И PYTHON ДЕРЖИТСЯ, И НИ ОДНА СТОРОНА НЕ ДУБЛИРУЕТ ДРУГУЮ.

    Граница объявлена в docs/BOUNDARY.md 13.09.2026 после вопроса ЛПР «как бинарник
    взаимодействует с ЛЛМ». Вопрос вскрыл не дефект, а НЕНАЗВАННОЕ: взаимодействие с
    моделью живёт в двух местах, и пока это не объявлено, каждый читатель считает по-своему.

    ПОЧЕМУ ГРАНИЦУ НАДО ПРОВЕРЯТЬ, А НЕ ПРОСТО ЗАПИСАТЬ. Сегодня трижды выяснилось, что
    правило, живущее документом или уроком, чинится в месте находки и уцелевает в
    следующем месте. Граница — тоже правило, и без проверки она продержится ровно до
    первого удобного случая написать «ну тут же быстрее свой клиент очереди».

    Дубль не усиливает, а создаёт расхождение — и расхождение обнаружится не там, где
    сделано. Это куплено сегодня трижды: на проверках, на путях поиска инструмента и на
    двух реализациях судьи.

    Три кода: 0 граница держится, 1 нарушена, 2 проверять нечем.
#>
[CmdletBinding()]
param([string]$ProductRoot = '')
if (-not $ProductRoot) { $ProductRoot = Split-Path -Parent $PSScriptRoot }

$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

$fail = @(); $unknown = @(); $ok = @()

$goDir = Join-Path $ProductRoot 'cmd'
$pyDir = Join-Path $ProductRoot 'skills\run-worker-task\scripts'
$doc   = Join-Path $ProductRoot 'docs\BOUNDARY.md'

if (-not (Test-Path -LiteralPath $doc)) {
    Write-Output "[FAIL] нечем проверить: границы нет на бумаге — $doc"
    exit 2
}
$ok += 'граница объявлена документом'

$goFiles = @(Get-ChildItem -LiteralPath $goDir -Filter *.go -File -ErrorAction SilentlyContinue |
             Where-Object { $_.Name -notlike '*_test.go' })
$pyFiles = @(Get-ChildItem -LiteralPath $pyDir -Filter *.py -File -ErrorAction SilentlyContinue)
if ($goFiles.Count -eq 0 -or $pyFiles.Count -eq 0) {
    Write-Output '[FAIL] нечем проверить: одна из сторон границы отсутствует на диске'
    exit 2
}
$ok += "сторон на месте: Go — $($goFiles.Count) файлов, Python — $($pyFiles.Count)"

function Get-Text([object[]]$files) {
    $sb = New-Object System.Text.StringBuilder
    foreach ($f in $files) { [void]$sb.AppendLine([IO.File]::ReadAllText($f.FullName, [Text.Encoding]::UTF8)) }
    return $sb.ToString()
}
$goText = Get-Text $goFiles
$pyText = Get-Text $pyFiles

# --- 1. Go не заводит своего клиента очереди --------------------------------
# Исполнение задач идёт через llm-queue — СОСЕДНИЙ продукт со своим объявленным CLI.
# Свой клиент в Go был бы второй реализацией одного правила на уровне продуктов.
$queueMarks = @('llm-queue', 'dispatcher', 'enqueue', 'llm_client', 'openrouter', 'air_llm_router')
$goQueue = @()
foreach ($m in $queueMarks) {
    # Упоминание в КОММЕНТАРИИ законно и даже полезно: там объясняется, чего Go не делает.
    foreach ($line in ($goText -split "`r?`n")) {
        $t = $line.Trim()
        if ($t.StartsWith('//')) { continue }
        if ($t -match [regex]::Escape($m)) { $goQueue += "$m в строке: $t"; break }
    }
}
if ($goQueue.Count) {
    $fail += ('Go тянется к очереди: ' + ($goQueue -join ' | ') + '. Нужен доступ к очереди — через скил, а не своим клиентом.')
} else {
    $ok += 'Go не заводит своего клиента очереди'
}

# --- 2. Python не заводит своего судьи, плана и расстояния до цели ----------
$judgeMarks = @('ЦЕЛЬ ДОСТИГНУТА', 'goal-drift', 'PLAN.md', 'расстояние до цели')
$pyJudge = @()
foreach ($m in $judgeMarks) {
    foreach ($line in ($pyText -split "`r?`n")) {
        $t = $line.Trim()
        if ($t.StartsWith('#')) { continue }
        if ($t -match [regex]::Escape($m)) { $pyJudge += "$m в строке: $t"; break }
    }
}
if ($pyJudge.Count) {
    $fail += ('Python завёл своё суждение о цели: ' + ($pyJudge -join ' | ') + '. Вердикт берётся у бинарника, а не считается второй раз.')
} else {
    $ok += 'Python не заводит своего судьи и своего расстояния до цели'
}

# --- 3. Обе точки входа существуют и отвечают ------------------------------
# Граница объявляет их две и разной природы: скил — для работы, бинарник — для управления.
$skill = Join-Path $ProductRoot 'skills\run-worker-task\SKILL.md'
if (-not (Test-Path -LiteralPath $skill)) {
    $fail += 'точки входа для сессии нет: отсутствует skills/run-worker-task/SKILL.md'
} else {
    $ok += 'точка входа для сессии на месте: скил run-worker-task'
}
$exe = Join-Path $ProductRoot 'bin\air-worker.exe'
if (-not (Test-Path -LiteralPath $exe)) {
    $unknown += 'точку входа для управления проверить нечем: бинарник не собран'
} else {
    $v = (& $exe version 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { $fail += "точка входа для управления не отвечает (код $LASTEXITCODE)" }
    else { $ok += "точка входа для управления отвечает: $v" }
}

if ($ok.Count -eq 0 -and $fail.Count -eq 0) { Write-Output '[FAIL] нечем проверить: ни одно утверждение не собралось'; exit 2 }
if ($unknown.Count) {
    foreach ($u in $unknown) { Write-Output "[FAIL] нечем проверить: $u" }
    foreach ($f in $fail) { Write-Output "[FAIL] $f" }
    exit 2
}
if ($fail.Count) {
    foreach ($o in $ok) { Write-Output "[PASS] $o" }
    foreach ($f in $fail) { Write-Output "[FAIL] $f" }
    exit 1
}
foreach ($o in $ok) { Write-Output "[PASS] $o" }
Write-Output '[PASS] граница держится: каждая сторона делает своё и не дублирует другую'
exit 0
