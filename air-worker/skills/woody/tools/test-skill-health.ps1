<#
.SYNOPSIS
  Здоровье скила: кодировки, синтаксис хуков, целость объявлений.

.DESCRIPTION
  Скил знает три правила, добытых прогоном, и до сих пор не проверял ни одного:
    - .cmd-обёртки только ASCII (cmd.exe читает их в OEM и исполняет мусор);
    - .ps1 с кириллицей сохраняются UTF-8 с BOM (иначе 5.1 читает как ANSI);
    - команды хуков во frontmatter должны существовать на диске.
  Правило, которое некому проверить, держится памятью — ровно то, от чего скил уходит.

  Возврат 0 — всё прошло, 1 — есть провал, 2 — проверять нечем.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$skillRoot = Split-Path -Parent $PSScriptRoot

$passed = 0
$failed = 0

function Assert-That([string]$title, [scriptblock]$check) {
    try {
        $verdict = & $check
        if ($verdict) { Write-Output "  [PASS]  $title"; $script:passed++ }
        else { Write-Output "  [FAIL]  $title"; $script:failed++ }
    } catch {
        Write-Output "  [FAIL]  $title -- $($_.Exception.Message)"
        $script:failed++
    }
}

$hookDir = Join-Path $skillRoot 'hooks'
if (-not (Test-Path -LiteralPath $hookDir)) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: каталога hooks нет'
    exit 2
}

Write-Output 'Здоровье скила air-woody'

# ИНВАРИАНТА ОХВАТА. Находка второй машины 12.09.2026: её проверка отрапортовала
# «НОРМА, проверок выполнено 6» с кодом 0, не коснувшись ни одного файла — в фильтре
# схлопнулось экранирование, список вышел пустым, цикл не выполнился, а исключения ушли
# в поток ошибок, который ветвь script не читает. Зелёный вердикт при пропущенном классе
# опаснее красного: красный зовут чинить, зелёный закрывает тему.
# Стоит ПОСЛЕ сбора и ДО циклов — иначе циклы просто не выполнятся и снова промолчат.
$wrappers = @(Get-ChildItem -LiteralPath $hookDir -Filter '*.cmd' -File)
$scripts  = @(Get-ChildItem -LiteralPath $skillRoot -Filter '*.ps1' -File -Recurse |
              Where-Object { $_.FullName -notmatch '\\\.git\\' })

if ($wrappers.Count -eq 0) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: обёрток .cmd не собрано ни одной — обход сломан, а не каталог пуст.'
    Write-Output 'Хуки объявлены во frontmatter и обязаны иметь обёртки; пустой охват здесь невозможен по устройству скила.'
    exit 2
}
if ($scripts.Count -eq 0) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: файлов .ps1 не собрано ни одного, хотя этот файл сам является .ps1 под тем же корнем.'
    exit 2
}
Write-Output ("  охват: обёрток {0}, сценариев {1}" -f $wrappers.Count, $scripts.Count)

# 1. Обёртки .cmd — только ASCII. Кириллица в них превращается cmd.exe в мусор и
#    исполняется как команда. Поймано прогоном, не чтением.
foreach ($wrapper in $wrappers) {
    Assert-That ("обёртка только ASCII: " + $wrapper.Name) {
        $bytes = [System.IO.File]::ReadAllBytes($wrapper.FullName)
        -not ($bytes | Where-Object { $_ -gt 127 })
    }.GetNewClosure()
}

# 2. .ps1 с кириллицей обязаны нести BOM, иначе PowerShell 5.1 читает их как ANSI.
foreach ($script in $scripts) {
    Assert-That ("BOM при кириллице: " + $script.Name) {
        $bytes = [System.IO.File]::ReadAllBytes($script.FullName)
        $hasBom = ($bytes.Length -ge 3) -and ($bytes[0] -eq 0xEF) -and ($bytes[1] -eq 0xBB) -and ($bytes[2] -eq 0xBF)
        $text = [System.Text.Encoding]::UTF8.GetString($bytes)
        $hasCyrillic = $text -match '[Ѐ-ӿ]'
        (-not $hasCyrillic) -or $hasBom
    }.GetNewClosure()
}

# 3. Каждый .ps1 разбирается синтаксически. Сломанный хук харнесс не починит, а
#    страж по своему же правилу пропускает при любой своей ошибке — то есть молча.
foreach ($script in $scripts) {
    Assert-That ("синтаксис разбирается: " + $script.Name) {
        $errors = $null
        $null = [System.Management.Automation.PSParser]::Tokenize(
            (Get-Content -LiteralPath $script.FullName -Raw -Encoding UTF8), [ref]$errors)
        (-not $errors) -or ($errors.Count -eq 0)
    }.GetNewClosure()
}

# 4. Каждая объявленная во frontmatter команда хука существует. Это тот же разрыв
#    «объявлено против работает», что закрыт в mode.ps1, но проверенный со стороны скила.
Assert-That 'все команды хуков из frontmatter существуют на диске' {
    $declared = @()
    foreach ($line in (Get-Content -LiteralPath (Join-Path $skillRoot 'SKILL.md') -Encoding UTF8)) {
        if ($line -match "^\s*-?\s*command:\s*'\""(.+?)\""'\s*$") { $declared += $Matches[1] }
        if ($line -match '^---\s*$' -and $declared.Count -gt 0) { break }
    }
    ($declared.Count -gt 0) -and (@($declared | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) }).Count -eq 0)
}

# 5. Каждая обёртка .cmd зовёт существующий .ps1 рядом с собой.
foreach ($wrapper in $wrappers) {
    Assert-That ("обёртка зовёт существующий .ps1: " + $wrapper.Name) {
        $body = Get-Content -LiteralPath $wrapper.FullName -Raw
        $hit = [regex]::Match($body, '([A-Za-z]:\\[^"''\s]+\.ps1)')
        (-not $hit.Success) -or (Test-Path -LiteralPath $hit.Groups[1].Value -PathType Leaf)
    }.GetNewClosure()
}

Write-Output ''
Write-Output ("прошло {0}, провалено {1}" -f $passed, $failed)
if ($failed -gt 0) { exit 1 }
exit 0
