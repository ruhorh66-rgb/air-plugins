<#
.SYNOPSIS
  Проверка судьи: результат воспроизводим в новой сессии на другой машине.

.DESCRIPTION
  Признак 7 постановки AIR-CHG-2026-000091 и главная жалоба ЛПР: «у нас куча продуктов,
  которые не доведены, я не могу воспроизвести результат в новой сессии».

  Прогонять здесь самого судью нельзя — он и зовёт эту проверку, вышла бы рекурсия.
  Поэтому проверяются ПРЕДПОСЫЛКИ воспроизводимости: чтобы тот же набор файлов дал тот
  же путь к вердикту, проверки продукта не должны зависеть от машины, от каталога
  запуска и от путей вне продукта.

  Основание — не теория. За одни сутки 12.09.2026 прибитые пути ломали работу трижды:
  провайдер air-postgres звал C:\Program Files\Python314 после переезда интерпретатора;
  верх лестницы самого air-worker звал claude.exe по адресу npm-установки, которой нет;
  конфигурация самотеста air-woody держала python314, а на второй машине стоит python312.
  Каждый раз отказ был МОЛЧАЛИВЫМ и выглядел как отказ продукта.

  Коды: 0 — предпосылки соблюдены; 1 — есть нарушение; 2 — проверять нечем.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Continue'

$productRoot = Split-Path -Parent $PSScriptRoot
$configPath = Join-Path $productRoot 'run-config.json'

if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: нет $configPath"
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

$cfg = $null
try { $cfg = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json } catch {
    Write-Output "НЕЧЕМ ПРОВЕРИТЬ: run-config.json не разбирается — $($_.Exception.Message)"
    exit 2
}

# --- 1. Все объявленные проверки существуют ------------------------------------------
$declared = @($cfg.judge.checks)
if ($declared.Count -eq 0) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: в конфигурации не объявлено ни одной проверки.'
    Write-Output 'Пустой список — это сломанная конфигурация, а не «всё воспроизводимо».'
    exit 2
}
Write-Output ("  охват: объявленных проверок {0}" -f $declared.Count)

foreach ($chk in $declared) {
    if (-not $chk.script) { continue }
    $name = [string]$chk.name
    $path = Join-Path $productRoot ([string]$chk.script)
    Assert-That ("проверка существует: $name") { Test-Path -LiteralPath $path -PathType Leaf }
}

# --- 2. Пути проверок — относительные, внутри продукта --------------------------------
foreach ($chk in $declared) {
    if (-not $chk.script) { continue }
    $name = [string]$chk.name
    $raw = [string]$chk.script
    Assert-That ("путь проверки относительный: $name") {
        -not ($raw -match '^[A-Za-z]:\\' -or $raw.StartsWith('\\') -or $raw.StartsWith('/'))
    }.GetNewClosure()
}

# --- 3. В самих проверках нет прибитых путей к чужим томам ----------------------------
# Собственные каталоги продукта законны; мина — абсолютный путь НАРУЖУ, к инструменту
# или данным, которых на другой машине не окажется по тому же адресу.
$toolFiles = @(Get-ChildItem -LiteralPath $PSScriptRoot -Filter '*.ps1' -File -ErrorAction SilentlyContinue)
if ($toolFiles.Count -eq 0) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: в tools нет ни одного сценария — обход сломан, а не каталог пуст.'
    exit 2
}
foreach ($tool in $toolFiles) {
    $text = [System.IO.File]::ReadAllText($tool.FullName, [System.Text.Encoding]::UTF8)
    Assert-That ("нет прибитого пути к интерпретатору: $($tool.Name)") {
        # Ищем присваивание абсолютного пути к исполняемому файлу. Упоминание пути в
        # КОММЕНТАРИИ законно: комментарий объясняет дефект, а не создаёт его — на этом
        # уже обожглась проверка no-pinned-root на второй машине, дав ложный красный
        # на собственном объяснении.
        $hits = @([regex]::Matches($text, '(?m)^\s*\$\w+\s*=\s*["\x27][A-Za-z]:\\[^"\x27]+\.(exe|cmd|bat)["\x27]'))
        $hits.Count -eq 0
    }.GetNewClosure()
}

# --- 4. Реестр фактов лежит с продуктом и под git -------------------------------------
$checklistRel = [string]$cfg.judge.checklist
Assert-That 'реестр фактов объявлен относительным путём внутри продукта' {
    $checklistRel -and -not ($checklistRel -match '^[A-Za-z]:\\')
}
Assert-That 'реестр фактов существует' {
    Test-Path -LiteralPath (Join-Path $productRoot $checklistRel) -PathType Leaf
}

# --- 5. План существует и не остался заготовкой ---------------------------------------
$planRel = if ($cfg.plan) { [string]$cfg.plan } else { 'PLAN.md' }
$planPath = Join-Path $productRoot $planRel
Assert-That 'план существует' { Test-Path -LiteralPath $planPath -PathType Leaf }
Assert-That 'план не остался незаполненной заготовкой' {
    if (-not (Test-Path -LiteralPath $planPath)) { return $false }
    $planText = [System.IO.File]::ReadAllText($planPath, [System.Text.Encoding]::UTF8)
    -not ($planText -match 'ЗАГОТОВКА-НЕ-ЗАПОЛНЕНА')
}

Write-Output ''
Write-Output ("прошло {0}, провалено {1}" -f $passed, $failed)
if ($failed -gt 0) { exit 1 }
exit 0
