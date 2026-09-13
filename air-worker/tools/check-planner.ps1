#requires -Version 5.1
<#
    Проверка: ПЛАНИРОВЩИК РАЗБИВАЕТ ЦЕЛЬ ОДНИМ ДОРОГИМ ВЫЗОВОМ И НЕ ОТНИМАЕТ ПЛАН.

    Шаг 5 плана. Пятая роль, названная ЛПР сверх четырёх ролей AUTO-080 и прямо
    отделённая им от планировщика ОС: «не нужен тебе планировщик вообще от слова
    совсем» — это про задачу Windows, а не про разбивку цели.

    Проверяются три свойства, и каждое отвечает на свой способ обмануться.

    ОДИН ВЫЗОВ. Разбивка окупается ровно потому, что она одна: ошибка в назначении
    ступени тиражируется на все последующие прогоны, а разведка боем на каждом шаге
    выглядит работой и стоит вдесятеро. Разбивка, потребовавшая пяти заходов, — это уже
    та самая разведка, от которой шаг защищает. Число вызовов берётся из замера, а не
    со слов.

    СТУПЕНИ НАЗНАЧЕНЫ, А НЕ ПРОСТАВЛЕНЫ ОДНОЙ. Разбивка, где всё `script`, и разбивка,
    где всё `opus`, одинаково означают, что назначения не было. Первая отдаст суждение
    скрипту, вторая заплатит дорогой моделью за переименование файлов. Поэтому
    требуется РАЗНООБРАЗИЕ ступеней — не как вкус, а как признак, что решение принимали.

    ПЛАН ОСТАЛСЯ У ЧЕЛОВЕКА. Главное. План правится руками: машина исполняет, человек
    владеет разбивкой. Планировщик, молча переписывающий PLAN.md, отнимает единственное
    место, где решает человек, — и отнимает тихо, потому что новый план выглядит как
    старый. Проверяется не обещание в документации, а ВЕТКА ОТКАЗА в коде.

    Охват обязателен: пустой собранный список — код 2, а не зелёный вердикт.
#>
[CmdletBinding()]
param([string]$ProductRoot = '')
if (-not $ProductRoot) { $ProductRoot = Split-Path -Parent $PSScriptRoot }

$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

$fail = @(); $unknown = @(); $ok = @()

$cfg = $null
try { $cfg = Get-Content -LiteralPath (Join-Path $ProductRoot 'run-config.json') -Raw -Encoding UTF8 | ConvertFrom-Json } catch { }
$ladder = if ($cfg) { @($cfg.ladder) } else { @() }
if (-not $ladder.Count) { $unknown += 'лестница продукта не прочитана: ступени сверять не с чем' }

# --- механизм на месте -------------------------------------------------------
$plannerPath = $null
foreach ($c in @(
    $env:WOODY_PLANNER,
    $(if ($env:CLAUDE_CONFIG_DIR) { Join-Path $env:CLAUDE_CONFIG_DIR 'skills\air-woody\scripts\planner.ps1' } else { $null }),
    'E:\-5-\014_Skills\air-woody\scripts\planner.ps1'
)) { if ($c -and (Test-Path -LiteralPath $c -PathType Leaf)) { $plannerPath = $c; break } }
if (-not $plannerPath) {
    $unknown += 'планировщик не найден: ни WOODY_PLANNER, ни скил air-woody, ни известное расположение'
} else {
    $ok += 'планировщик резолвится'
}

# --- один вызов, и это видно из замера ---------------------------------------
$ansPath = Join-Path $ProductRoot '.woody\planner-answer.json'
if (-not (Test-Path -LiteralPath $ansPath)) {
    $fail += "ответа разбивщика нет ($ansPath): планировщик ни разу не отработал на этом продукте"
} else {
    try {
        $ans = Get-Content -LiteralPath $ansPath -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($ans.is_error) { $fail += "последняя разбивка окончилась отказом: $($ans.result)" }
        else { $ok += "разбивка состоялась одним вызовом, ходов $($ans.num_turns)" }
        # Цена называется, а не подразумевается — норма AIR_VIBECODING.
        if ($null -eq $ans.total_cost_usd) { $fail += 'замер разбивки не записан: цена дорогого вызова не названа' }
    } catch { $fail += 'ответ разбивщика не разобран' }
}

# --- предложение читаемо петлёй и ступени назначены ---------------------------
$prop = Join-Path $ProductRoot 'PLAN.proposed.md'
if (-not (Test-Path -LiteralPath $prop)) {
    $fail += "предложения нет ($prop): разбивка никуда не легла"
} else {
    $rows = @()
    foreach ($line in (Get-Content -LiteralPath $prop -Encoding UTF8)) {
        if ($line -match '^\s*\|\s*(~~)?\s*\d+\s*(~~)?\s*\|') { $rows += $line }
    }
    if (-not $rows.Count) {
        $fail += 'в предложении нет ни одной строки шага'
    } else {
        $ok += "предложение содержит шагов: $($rows.Count)"
        $cols = (($rows[0].Trim().Trim('|')) -split '\|').Count
        if ($cols -ne 4) {
            $fail += "в предложении $cols колонок вместо четырёх: петля разберёт такой план в НОЛЬ шагов и скажет «план пуст» — молча"
        } else { $ok += 'грамматика предложения — четыре колонки, петля разберёт' }

        $tiers = @()
        $bad = @()
        foreach ($r in $rows) {
            $parts = ($r.Trim().Trim('|')) -split '\|'
            if ($parts.Count -lt 3) { continue }
            $t = $parts[2].Trim().Trim('`').Trim()
            if ($t -eq '—' -or $t -eq '-' -or $r -match 'гейт') { continue }
            $tiers += $t
            if ($ladder.Count -and ($ladder -notcontains $t) -and
                ($ladder -notcontains ($t -split ':')[0]) -and
                (-not @($ladder | Where-Object { ($_ -split ':')[0] -eq ($t -split ':')[0] }).Count)) { $bad += $t }
        }
        if ($bad.Count) { $fail += ('назначены ступени вне лестницы: ' + (($bad | Select-Object -Unique) -join ', ')) }
        elseif ($tiers.Count) { $ok += 'все назначенные ступени есть в лестнице продукта' }

        $distinct = @($tiers | Select-Object -Unique).Count
        if ($tiers.Count -ge 3 -and $distinct -lt 2) {
            $fail += "все исполняемые шаги получили одну ступень ($($tiers[0])): это не назначение, а умолчание"
        } elseif ($distinct -ge 2) {
            $ok += "ступени различаются: $distinct разных на $($tiers.Count) исполняемых шагов"
        }
    }
}

# --- план остался у человека: проверяется веткой отказа, а не обещанием -------
if ($plannerPath) {
    $src = Get-Content -LiteralPath $plannerPath -Raw -Encoding UTF8
    if ($src -notmatch 'PLAN\.proposed\.md') {
        $fail += 'планировщик не кладёт предложение отдельным файлом: значит пишет прямо в план'
    }
    # Ветка обязана существовать: «PLAN.md есть -> не трогаю». Ищется рядом с проверкой
    # существования плана, а не по всему файлу: упоминание без ветки — это обещание.
    $hasRefuse = $false
    foreach ($m in [regex]::Matches($src, '(?s)Test-Path[^\r\n]{0,80}\$planPath')) {
        $tail = $src.Substring($m.Index, [Math]::Min(600, $src.Length - $m.Index))
        if ($tail -match 'НЕ ТРОГАЮ|не трогаю') { $hasRefuse = $true; break }
    }
    if ($hasRefuse) { $ok += 'существующий PLAN.md планировщиком не переписывается' }
    else { $fail += 'у планировщика нет ветки отказа переписывать существующий PLAN.md: разбивка перестаёт принадлежать человеку' }
}

if ($ok.Count -eq 0 -and $fail.Count -eq 0) { Write-Output '[FAIL] нечем проверить: ни одна проверка планировщика не собралась'; exit 2 }
if ($unknown.Count) {
    foreach ($u in $unknown) { Write-Output "[FAIL] нечем проверить: $u" }
    foreach ($f in $fail) { Write-Output "[FAIL] $f" }
    exit 2
}
if ($fail.Count) { foreach ($f in $fail) { Write-Output "[FAIL] $f" }; exit 1 }
foreach ($o in $ok) { Write-Output "[PASS] $o" }
Write-Output "[PASS] планировщик разбивает одним вызовом, назначает ступени и не отнимает план у человека"
exit 0
