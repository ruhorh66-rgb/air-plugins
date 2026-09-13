<#
.SYNOPSIS
  Проверка предполётного отказа mode.ps1 и различения «включён» / «исполняется».

.DESCRIPTION
  Закрывает находку 12.09.2026 со второй машины: команды хуков объявлены во frontmatter
  АБСОЛЮТНЫМ путём, а регистрация скила идёт через <CLAUDE_CONFIG_DIR>\skills\air-woody.
  Пути разные и расходятся молча — скил регистрируется, хуки указывают в пустоту.

  Тест не читает код, а ПРОГОНЯЕТ mode.ps1 в подставном дереве: так проверяется
  поведение, а не намерение. Возврат 0 — все проверки прошли, 1 — есть провал.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$skillRoot = Split-Path -Parent $PSScriptRoot
$modeScript = Join-Path $skillRoot 'hooks\mode.ps1'

$passed = 0
$failed = 0

function Assert-That([string]$title, [scriptblock]$check) {
    try {
        $verdict = & $check
        # Метка ASCII идёт ПЕРВОЙ намеренно: судья зовёт проверку через Start-Process с
        # перенаправлением, и кириллица в перехваченном выводе превращается в мусор. По
        # счёту «провалено 1» не видно, какая именно упала; по [FAIL] — видно всегда.
        if ($verdict) { Write-Output "  [PASS]  ПРОШЛА  $title"; $script:passed++ }
        else { Write-Output "  [FAIL]  ПРОВАЛ  $title"; $script:failed++ }
    } catch {
        Write-Output "  [FAIL]  ПРОВАЛ  $title -- $($_.Exception.Message)"
        $script:failed++
    }
}

# Подставное дерево: hooks\mode.ps1 рядом с SKILL.md, объявляющим НЕСУЩЕСТВУЮЩУЮ команду.
# mode.ps1 ищет SKILL.md относительно собственного расположения, поэтому копии достаточно.
function New-FakeSkill([string]$commandPath) {
    $base = Join-Path ([System.IO.Path]::GetTempPath()) ("woody-test-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Force -Path (Join-Path $base 'hooks') | Out-Null
    Copy-Item -LiteralPath $modeScript -Destination (Join-Path $base 'hooks\mode.ps1')
    $skillText = @"
---
name: air-woody-test
description: 'подставной скил для проверки предполётного отказа'
hooks:
  Stop:
    - hooks:
        - type: command
          command: '"$commandPath"'
          timeout: 20
---

# подставной
"@
    Set-Content -LiteralPath (Join-Path $base 'SKILL.md') -Value $skillText -Encoding UTF8
    return $base
}

Write-Output 'Проверка предполётного отказа mode.ps1'

# 1. Настоящий скил: все объявленные команды на месте.
Assert-That 'объявленные команды хуков настоящего скила существуют на диске' {
    $declared = @()
    foreach ($line in (Get-Content -LiteralPath (Join-Path $skillRoot 'SKILL.md') -Encoding UTF8)) {
        if ($line -match "^\s*-?\s*command:\s*'\""(.+?)\""'\s*$") { $declared += $Matches[1] }
        if ($line -match '^---\s*$' -and $declared.Count -gt 0) { break }
    }
    ($declared.Count -gt 0) -and (@($declared | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) }).Count -eq 0)
}

# 2. Команда объявлена, но её нет: включение обязано ОТКАЗАТЬ, а не записать обещание.
$fakeMissing = New-FakeSkill 'Z:\заведомо-нет-такого\turn-guard.cmd'
try {
    Assert-That 'при несуществующей команде хука -On отказывает ненулевым кодом' {
        $env:CLAUDE_CODE_SESSION_ID = 'woody-test-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
        $out = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $fakeMissing 'hooks\mode.ps1') -On 2>&1
        $code = $LASTEXITCODE
        $stateFile = Join-Path $env:ProgramData ("AIR OS\State\woody-mode-$($env:CLAUDE_CODE_SESSION_ID).json")
        # Сверяем КОД и ОТСУТСТВИЕ ФАЙЛА, а не текст отказа. Первая версия искала слово
        # «ОТКАЗ» в выводе и падала под судьёй: тот зовёт проверку через Start-Process с
        # перенаправлением, дочерний powershell печатает кириллицу в консольной кодировке,
        # и совпадения не было. Прямой запуск это скрывал. Код и файл от кодировки не зависят.
        $null = $out
        $noFile = -not (Test-Path -LiteralPath $stateFile)
        if (Test-Path -LiteralPath $stateFile) { Remove-Item -LiteralPath $stateFile -Force }
        ($code -ne 0) -and $noFile
    }
} finally {
    Remove-Item -LiteralPath $fakeMissing -Recurse -Force -ErrorAction SilentlyContinue
}

# 3. Отсутствие объявленных команд — это «нечем проверить» (код 2), а не «всё хорошо».
$fakeEmpty = Join-Path ([System.IO.Path]::GetTempPath()) ("woody-test-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path (Join-Path $fakeEmpty 'hooks') | Out-Null
Copy-Item -LiteralPath $modeScript -Destination (Join-Path $fakeEmpty 'hooks\mode.ps1')
Set-Content -LiteralPath (Join-Path $fakeEmpty 'SKILL.md') -Value "---`nname: x`n---`n" -Encoding UTF8
try {
    Assert-That 'при отсутствии объявленных хуков -On возвращает 2, а не 0' {
        $env:CLAUDE_CODE_SESSION_ID = 'woody-test-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
        & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $fakeEmpty 'hooks\mode.ps1') -On 2>&1 | Out-Null
        $code = $LASTEXITCODE
        $stateFile = Join-Path $env:ProgramData ("AIR OS\State\woody-mode-$($env:CLAUDE_CODE_SESSION_ID).json")
        if (Test-Path -LiteralPath $stateFile) { Remove-Item -LiteralPath $stateFile -Force }
        $code -eq 2
    }
} finally {
    Remove-Item -LiteralPath $fakeEmpty -Recurse -Force -ErrorAction SilentlyContinue
}

# 4. Состояние различает «включён» и «исполняется»: без свежей записи зонда -Status обязан
#    сказать это прямо, иначе повторяется потеря 11-12.09 — файл обещал, механизма не было.
Assert-That 'mode.ps1 умеет сообщать о неподтверждённой регистрации' {
    (Get-Content -LiteralPath $modeScript -Raw -Encoding UTF8) -match 'РЕГИСТРАЦИЯ НЕ ПОДТВЕРЖДЕНА'
}

Write-Output ''
Write-Output ("прошло {0}, провалено {1}" -f $passed, $failed)
if ($failed -gt 0) { exit 1 }
exit 0
