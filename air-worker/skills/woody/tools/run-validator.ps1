<#
.SYNOPSIS
  Запуск валидатора скила интерпретатором, найденным ПО ОТВЕТУ.

.DESCRIPTION
  Первая версия конфигурации самотеста держала прибитый путь
  R:\_tools\python314\python.exe. На второй машине такого файла нет — там python312, —
  и самотест не воспроизводился, хотя ради воспроизводимости и заводился. Тот же класс,
  что уже дважды оплачен: помощник пароля restic с прибитым путём и провайдер
  air-postgres, умерший от переезда Python.

  Очевидная починка «взять python из PATH» делает хуже. В %LOCALAPPDATA%\Microsoft\
  WindowsApps лежит алиас-заглушка магазина: файл существует, Get-Command его находит,
  вызов отвечает «Python was not found» кодом 9009. Ветвь «нечем проверить» не
  срабатывает, и отсутствие инструмента превращается в претензию к продукту.

  Отсюда правило: РЕЗОЛВ НЕ ДОКАЗЫВАЕТ НАЛИЧИЯ ИНСТРУМЕНТА, ДОКАЗЫВАЕТ ТОЛЬКО ОТВЕТ.
  Кандидат считается интерпретатором, если на -V он ответил строкой «Python N».

  Коды: 0 — валидатор прошёл, 1 — не прошёл, 2 — проверять нечем.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$skillRoot = Split-Path -Parent $PSScriptRoot
$validator = Join-Path $skillRoot 'tools\validate-skill.py'

if (-not (Test-Path -LiteralPath $validator -PathType Leaf)) {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: нет $validator"
    exit 2
}

# Кандидаты: сперва то, что на PATH, затем известные расположения. Порядок не важен —
# берётся первый ОТВЕТИВШИЙ, а не первый существующий.
$candidates = @()
$onPath = Get-Command 'python' -ErrorAction SilentlyContinue
if ($onPath -and $onPath.Source) { $candidates += [string]$onPath.Source }
$onPath3 = Get-Command 'python3' -ErrorAction SilentlyContinue
if ($onPath3 -and $onPath3.Source) { $candidates += [string]$onPath3.Source }

foreach ($base in @('R:\_tools', 'C:\Program Files', "$env:LOCALAPPDATA\Programs\Python")) {
    if (-not (Test-Path -LiteralPath $base)) { continue }
    foreach ($dir in Get-ChildItem -LiteralPath $base -Directory -Filter 'Python3*' -ErrorAction SilentlyContinue) {
        $exe = Join-Path $dir.FullName 'python.exe'
        if (Test-Path -LiteralPath $exe -PathType Leaf) { $candidates += $exe }
    }
    foreach ($dir in Get-ChildItem -LiteralPath $base -Directory -Filter 'python3*' -ErrorAction SilentlyContinue) {
        $exe = Join-Path $dir.FullName 'python.exe'
        if (Test-Path -LiteralPath $exe -PathType Leaf) { $candidates += $exe }
    }
}

$interpreter = $null
foreach ($candidate in ($candidates | Select-Object -Unique)) {
    try {
        $answer = & $candidate -V 2>&1 | Select-Object -First 1
        if ($LASTEXITCODE -eq 0 -and "$answer" -match '^Python\s+\d') {
            $interpreter = $candidate
            Write-Output "интерпретатор: $candidate ($answer)"
            break
        }
        Write-Output "  пропущен (ответ не подошёл): $candidate"
    } catch {
        Write-Output "  пропущен (не ответил): $candidate"
    }
}

if (-not $interpreter) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: ни один кандидат не ответил строкой «Python N».'
    Write-Output 'Это не «валидатор не прошёл», а «его нечем запустить» — коды различаются намеренно.'
    exit 2
}

& $interpreter $validator
exit $LASTEXITCODE
