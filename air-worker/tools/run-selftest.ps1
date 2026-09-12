<#
.SYNOPSIS
  Проверка судьи: самотест air-worker проходит целиком.

.DESCRIPTION
  Обёртка над skills/run-worker-task/scripts/selftest.py — 36 проверок продукта:
  подпись заявки, приватность, free-only, лестница, протокол, метрики.

  Три кода различаются намеренно, как требует судья Вуди:
    0 — самотест прошёл;
    1 — самотест не прошёл, есть над чем работать;
    2 — ПРОВЕРЯТЬ НЕЧЕМ: не найден интерпретатор или сам файл самотеста.
  Отсутствие интерпретатора — это «нечем проверить», а не «продукт сломан»; смешать их
  значит поднять ступень и заплатить за ту же ошибку дороже.

  Интерпретатор ищется ПО ОТВЕТУ, а не прибитым путём: на этой машине за сутки дважды
  ломались прибитые пути (провайдер air-postgres и верх лестницы самого air-worker).
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Continue'

$productRoot = Split-Path -Parent $PSScriptRoot
$scriptsDir = Join-Path $productRoot 'skills\run-worker-task\scripts'
$selftest = Join-Path $scriptsDir 'selftest.py'

if (-not (Test-Path -LiteralPath $selftest -PathType Leaf)) {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: нет $selftest"
    exit 2
}

# Кандидаты перебираются, берётся первый ОТВЕТИВШИЙ строкой «Python N».
$candidates = @()
foreach ($name in @('python', 'python3')) {
    $found = Get-Command $name -ErrorAction SilentlyContinue
    if ($found -and $found.Source) { $candidates += [string]$found.Source }
}
foreach ($base in @('R:\_tools', 'C:\Program Files', "$env:LOCALAPPDATA\Programs\Python")) {
    if (-not (Test-Path -LiteralPath $base)) { continue }
    foreach ($dir in Get-ChildItem -LiteralPath $base -Directory -ErrorAction SilentlyContinue |
             Where-Object { $_.Name -match '^[Pp]ython3' }) {
        $exe = Join-Path $dir.FullName 'python.exe'
        if (Test-Path -LiteralPath $exe -PathType Leaf) { $candidates += $exe }
    }
}

$interpreter = $null
foreach ($candidate in ($candidates | Select-Object -Unique)) {
    try {
        $answer = & $candidate -V 2>&1 | Select-Object -First 1
        if ($LASTEXITCODE -eq 0 -and "$answer" -match '^Python\s+\d') { $interpreter = $candidate; break }
    } catch { }
}

if (-not $interpreter) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: ни один кандидат не ответил строкой «Python N».'
    Write-Output 'Это не «самотест провалился», а «его нечем запустить» — коды различаются намеренно.'
    exit 2
}

Write-Output "интерпретатор: $interpreter"
Push-Location $scriptsDir
try {
    $out = & $interpreter selftest.py 2>&1 | Out-String
    $code = $LASTEXITCODE
} finally {
    Pop-Location
}

# Итоговая строка самотеста выводится ВСЕГДА: судья перенаправляет вывод проверки, и без
# неё до человека дойдёт только код возврата без причины.
$tail = @($out -split "`r?`n" | Where-Object { $_ -match 'проверок|провал' }) | Select-Object -Last 1
if ($tail) { Write-Output ("  " + $tail.Trim()) }

if ($code -ne 0) {
    foreach ($line in ($out -split "`r?`n" | Where-Object { $_ -match '^FAIL' } | Select-Object -First 3)) {
        Write-Output ("  [FAIL] " + $line.Trim())
    }
    exit 1
}
exit 0
