#requires -Version 5.1
<#
    Проверка: ПЛАГИН КАНОНИЧЕН И ПЕРЕНОСИМ.

    Заведено 13.09.2026 по указанию ЛПР «привести к каноническому виду как плагин, учитывая
    все дыры, которые мы сейчас всплыли».

    Главная из этих дыр — ПРИБИТЫЕ К ОДНОЙ МАШИНЕ ПУТИ. Её нашла AIR-ENV-002 в goal.json:
    продукт про двигатель цели имел цель, написанную под один хост. Здесь тот же класс был
    опаснее: хуки объявлялись во frontmatter скила абсолютным путём машины разработчика, и
    на второй машине плагин зарегистрировал бы их В ПУСТОТУ — молча, потому что
    несработавший обработчик ничем не отличается от отсутствующего. В goal.json отказ был
    хотя бы виден судьёй; здесь не виден ничем.

    Поэтому проверка ищет не «файлы на месте», а ПЕРЕНОСИМОСТЬ: абсолютный путь с буквой
    диска в любом объявлении плагина — отказ, независимо от того, существует он сегодня на
    этой машине или нет. Существующий чужой путь врёт убедительнее отсутствующего — это мы
    уже покупали.

    Три кода, как у судьи: 0 канонично, 1 нет, 2 проверять нечем.
#>
[CmdletBinding()]
param([string]$ProductRoot = '')
if (-not $ProductRoot) { $ProductRoot = Split-Path -Parent $PSScriptRoot }

$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

$fail = @(); $unknown = @(); $ok = @()

# --- 1. манифест ------------------------------------------------------------
$manifest = Join-Path $ProductRoot '.claude-plugin\plugin.json'
if (-not (Test-Path -LiteralPath $manifest)) {
    Write-Output "[FAIL] нечем проверить: нет $manifest — это не плагин"
    exit 2
}
$pl = $null
try { $pl = Get-Content -LiteralPath $manifest -Raw -Encoding UTF8 | ConvertFrom-Json } catch {
    Write-Output "[FAIL] нечем проверить: манифест не разобран — $($_.Exception.Message)"
    exit 2
}
foreach ($f in @('name', 'version', 'description')) {
    if ([string]::IsNullOrWhiteSpace([string]$pl.$f)) { $fail += "манифест не называет поле $f" }
}
if ($fail.Count -eq 0) { $ok += "манифест полон: $($pl.name) $($pl.version)" }

# Версия манифеста и версия бинарника — одно и то же число, иначе выпуск говорит одно, а
# механизм другое, и по какому из них судить, человек решает гаданием.
$exe = Join-Path $ProductRoot 'bin\air-worker.exe'
if (-not (Test-Path -LiteralPath $exe)) {
    $unknown += 'бинарника нет — версию манифеста сверить не с чем'
} else {
    $v = (& $exe version 2>&1 | Out-String).Trim()
    if ($v -notmatch [regex]::Escape([string]$pl.version)) {
        $fail += "версия манифеста ($($pl.version)) не совпадает с версией бинарника ($v)"
    } else {
        $ok += "версия манифеста и бинарника совпадают: $($pl.version)"
    }
}

# --- 2. скилы объявлены каталогом, как требует канон -------------------------
$skillsDir = Join-Path $ProductRoot 'skills'
$skills = @(Get-ChildItem -LiteralPath $skillsDir -Directory -ErrorAction SilentlyContinue |
            Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName 'SKILL.md') })
if ($skills.Count -eq 0) {
    $fail += "в $skillsDir нет ни одного каталога со SKILL.md"
} else {
    $ok += ('скилов в плагине: ' + $skills.Count + ' (' + (($skills | ForEach-Object { $_.Name }) -join ', ') + ')')
}

# --- 3. ПЕРЕНОСИМОСТЬ: ни одного абсолютного пути в объявлениях ---------------
# Смотрим ровно туда, что читает харнесс при установке: манифест, объявление хуков и
# frontmatter скилов. Тело скила — документация, там абсолютный путь может быть примером.
$declared = New-Object System.Collections.Generic.List[object]
$declared.Add([pscustomobject]@{ Name = '.claude-plugin/plugin.json'; Path = $manifest })
$hooksJson = Join-Path $ProductRoot 'hooks\hooks.json'
if (Test-Path -LiteralPath $hooksJson) { $declared.Add([pscustomobject]@{ Name = 'hooks/hooks.json'; Path = $hooksJson }) }
foreach ($s in $skills) {
    $declared.Add([pscustomobject]@{ Name = "skills/$($s.Name)/SKILL.md (frontmatter)"; Path = (Join-Path $s.FullName 'SKILL.md'); FrontOnly = $true })
}

$reAbs = '[A-Za-z]:\\'
foreach ($d in $declared) {
    $text = ''
    try { $text = Get-Content -LiteralPath $d.Path -Raw -Encoding UTF8 } catch { }
    if ($d.FrontOnly) {
        # Только frontmatter: он и есть объявление. Всё после второго --- — текст для человека.
        $m = [regex]::Match($text, '(?s)^﻿?---\r?\n(.*?)\r?\n---')
        $text = if ($m.Success) { $m.Groups[1].Value } else { '' }
    }
    $hits = @([regex]::Matches($text, $reAbs))
    if ($hits.Count -gt 0) {
        $sample = ([regex]::Match($text, '[A-Za-z]:\\[^\s"'',]*')).Value
        $fail += "$($d.Name): абсолютный путь в объявлении ($sample). На другой машине это объявление ведёт в пустоту."
    } else {
        $ok += "$($d.Name): абсолютных путей нет"
    }
}

# --- 4. хуки объявлены плагином и их файлы существуют ------------------------
if (-not (Test-Path -LiteralPath $hooksJson)) {
    $fail += 'hooks/hooks.json нет: страж режима не поедет с плагином, а значит режим снова будет держаться памятью'
} else {
    $hj = $null
    try { $hj = Get-Content -LiteralPath $hooksJson -Raw -Encoding UTF8 | ConvertFrom-Json } catch {
        $fail += "hooks/hooks.json не разобран: $($_.Exception.Message)"
    }
    if ($hj) {
        $cmds = @([regex]::Matches(($hj | ConvertTo-Json -Depth 8), '\$\{CLAUDE_PLUGIN_ROOT\}/([^"\\]+)'))
        if ($cmds.Count -eq 0) {
            $fail += 'в hooks/hooks.json нет ни одной команды через ${CLAUDE_PLUGIN_ROOT} — объявление непереносимо'
        } else {
            $missing = @()
            foreach ($m in $cmds) {
                $rel = $m.Groups[1].Value -replace '/', '\'
                if (-not (Test-Path -LiteralPath (Join-Path $ProductRoot $rel))) { $missing += $rel }
            }
            if ($missing.Count) {
                $fail += ('объявленных обработчиков нет на месте: ' + (($missing | Select-Object -Unique) -join ', '))
            } else {
                $ok += "хуки объявлены плагином переносимым путём, обработчиков на месте: $($cmds.Count)"
            }
        }
    }
}

# --- 5. запись в маркетплейсе совпадает с манифестом -------------------------
$market = Join-Path (Split-Path -Parent $ProductRoot) '.claude-plugin\marketplace.json'
if (-not (Test-Path -LiteralPath $market)) {
    $unknown += 'записи маркетплейса нет рядом — сверить нечем'
} else {
    $mk = $null
    try { $mk = Get-Content -LiteralPath $market -Raw -Encoding UTF8 | ConvertFrom-Json } catch { }
    if (-not $mk) { $unknown += 'запись маркетплейса не разобрана' }
    else {
        $entry = @($mk.plugins | Where-Object { $_.name -eq [string]$pl.name })
        if ($entry.Count -eq 0) { $fail += "в маркетплейсе нет записи о плагине $($pl.name)" }
        else { $ok += "плагин объявлен в маркетплейсе: $($entry[0].source)" }
    }
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
Write-Output '[PASS] плагин каноничен и переносим: ни одно объявление не привязано к машине'
exit 0
