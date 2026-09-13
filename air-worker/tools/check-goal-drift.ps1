#requires -Version 5.1
<#
    Проверка: ДВИГАТЕЛЬ ЦЕЛИ ЖИВ И ЕГО ВЕРДИКТ ИМЕЕТ ДЕЙСТВИЕ.

    Механизм восстановлен 13.09.2026 из Goal/Drift Loop ВЕРЫ
    (E:\-8-\air-vera\automation\controller\goal_drift.py) по прямому указанию ЛПР.
    Правило, ради которого он существует: рост промежуточного числа продвижением не
    считается, если расстояние до цели не уменьшается. Уход в сторону и сон — одно и то
    же явление в этих числах, и потому лечатся одним механизмом, а не двумя догадками.

    Проверка намеренно проверяет ЧЕТЫРЕ разные вещи, и четвёртая — главная.

    Первые три отвечают на «механизм есть и считает»: скрипт резолвится, судья кладёт
    машинный вердикт числами, история замеров ведётся и дописывается.

    Четвёртая отвечает на «его вердикт кого-нибудь останавливает». Именно здесь у ВЕРЫ
    был найденный контролёром дефект, записанный в её коде дословно: вердикт объявлялся,
    а цикл продолжал брать постороннюю работу и кончался PASS — «человеческий гейт,
    который Goal/Drift объявил, не имел никакого действия на плоскость управления».
    Механизм, чью эскалацию никто не исполняет, неотличим от отсутствующего, а стоит
    дороже: он создаёт уверенность. Поэтому здесь проверяется не наличие файла, а
    наличие ВЕТКИ ОСТАНОВКИ у тех, кто его зовёт.

    Охват обязателен: пустой собранный список — код 2 «нечем проверить», а не зелёный
    вердикт. Отсутствие доказательства нулевой оценкой не является.
#>
[CmdletBinding()]
param([string]$ProductRoot = '')
# $PSScriptRoot в значении параметра по умолчанию на 5.1 пуст — разрешается в теле.
if (-not $ProductRoot) { $ProductRoot = Split-Path -Parent $PSScriptRoot }

$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

$fail = @()
$unknown = @()
$checked = @()

# --- где живёт механизм ------------------------------------------------------
# Ищется по ответу, а не по прибитому пути: переменная окружения, потом каталог скила
# рядом с судьёй, потом известное расположение. Не нашли — «нечем проверить».
$driftPath = $null
foreach ($cand in @(
    $env:WOODY_GOAL_DRIFT,
    $(if ($env:CLAUDE_CONFIG_DIR) { Join-Path $env:CLAUDE_CONFIG_DIR 'skills\air-woody\scripts\goal-drift.ps1' } else { $null }),
    'F:\-7-\air-worker\skills\woody\scripts\goal-drift.ps1'
)) {
    if ($cand -and (Test-Path -LiteralPath $cand -PathType Leaf)) { $driftPath = $cand; break }
}
if (-not $driftPath) {
    $unknown += 'двигатель цели не найден: ни WOODY_GOAL_DRIFT, ни скил air-woody, ни известное расположение'
} else {
    $checked += 'скрипт двигателя резолвится'
}

# --- 1. судья кладёт числа, а не только текст --------------------------------
$vPath = Join-Path $ProductRoot '.goal-verdict.json'
if (-not (Test-Path -LiteralPath $vPath)) {
    $unknown += "машинного вердикта нет ($vPath): судья продукта ни разу не прогонялся новой редакцией"
} else {
    $checked += 'машинный вердикт судьи на месте'
    try {
        $v = Get-Content -LiteralPath $vPath -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($null -eq $v.code) { $fail += 'машинный вердикт без кода: расстояние не с чем сверять' }
        # distance допустимо null — это код 2, «нечем проверить». Требовать число здесь
        # значило бы требовать от судьи выдумать оценку там, где мерить нечем.
        if ($null -eq $v.facts_required) { $fail += 'машинный вердикт не называет требуемого числа фактов' }
    } catch {
        $fail += "машинный вердикт не разобран: $($_.Exception.Message)"
    }
}

# --- 2. история замеров ведётся и дописывается -------------------------------
$hist = Join-Path $ProductRoot '.woody\goal-drift.jsonl'
if (-not (Test-Path -LiteralPath $hist)) {
    $fail += "истории замеров нет ($hist): без неё застой не считается, и каждый ход судит сам себя заново"
} else {
    $checked += 'история замеров ведётся'
    $lines = @(Get-Content -LiteralPath $hist -Encoding UTF8 | Where-Object { $_.Trim() })
    if ($lines.Count -lt 1) {
        $fail += 'история замеров пуста: файл есть, а ни одного замера не записано'
    } else {
        try {
            $last = $lines[-1] | ConvertFrom-Json
            foreach ($k in @('distance', 'stall_moves', 'verdict')) {
                if (-not ($last.PSObject.Properties.Name -contains $k)) {
                    $fail += "в записи истории нет поля $k"
                }
            }
        } catch { $fail += 'последняя запись истории не разобрана' }
    }
}

# --- 3. вердикт имеет действие: у зовущих есть ветка остановки ----------------
# Проверяется не «файл упомянут», а «код возврата 2 приводит к остановке». Слабее было
# бы бессмысленно: упоминание без ветки — это ровно тот декоративный гейт, от которого
# механизм и восстанавливается.
if ($driftPath) {
    $skillRoot = Split-Path -Parent (Split-Path -Parent $driftPath)
    $callers = @(
        @{ Path = (Join-Path $skillRoot 'scripts\woody.ps1');     What = 'петля'; Stop = 'Close-Woody' },
        @{ Path = (Join-Path $skillRoot 'hooks\turn-guard.ps1');  What = 'страж'; Stop = '\$missing \+=' }
    )
    foreach ($c in $callers) {
        if (-not (Test-Path -LiteralPath $c.Path -PathType Leaf)) {
            $unknown += "$($c.What) не найден по пути $($c.Path): нечем проверить, исполняется ли эскалация"
            continue
        }
        $text = Get-Content -LiteralPath $c.Path -Raw -Encoding UTF8
        if ($text -notmatch 'goal-drift\.ps1') {
            $fail += "$($c.What) не зовёт двигатель цели вовсе: механизм есть, а спрашивать его никто не спрашивает"
            continue
        }
        # Ветка остановки ищется в окне после проверки кода 2 — так отличается настоящая
        # обработка от простого упоминания где-то в файле.
        # Окно берётся от МЕСТА ВЫЗОВА двигателя, а не от точного написания сравнения:
        # код возврата кладут то в $proc.ExitCode, то в промежуточную переменную, и
        # проверка, зелёная лишь при одном написании, ловит опечатку вместо дефекта.
        # Поймано этой же проверкой на первом прогоне 13.09.2026.
        # Окно берётся от МЕСТА ЗАПУСКА двигателя, а не от упоминания его файла и не от
        # точного написания сравнения. Обе прежние редакции дали ложный [FAIL] на живом,
        # исправном коде 13.09.2026: первая искала «ExitCode -eq 2», тогда как страж
        # кладёт код в промежуточную переменную; вторая мерила окно от имени файла, а
        # петля резолвит путь один раз наверху и запускает процесс сотнями строк ниже.
        # Проверять надо структуру — «двигатель запущен, и рядом ветка на код 2, которая
        # останавливает», — потому что именно она и есть предмет проверки.
        $hasStop = $false
        foreach ($m in [regex]::Matches($text, '(?s)Start-Process(?:(?!Start-Process).){0,800}?drift')) {
            $tail = $text.Substring($m.Index, [Math]::Min(2500, $text.Length - $m.Index))
            if (($tail -match '-eq\s+2') -and ($tail -match $c.Stop)) { $hasStop = $true; break }
        }
        if ($hasStop) { $checked += "$($c.What) останавливается по эскалации двигателя" }
        else { $fail += "$($c.What) зовёт двигатель, но эскалацию не исполняет: вердикт без действия" }
    }
}

# --- инварианта охвата -------------------------------------------------------
if ($checked.Count -eq 0 -and $fail.Count -eq 0) {
    Write-Output '[FAIL] нечем проверить: ни одна проверка двигателя цели не собралась'
    exit 2
}
if ($unknown.Count) {
    foreach ($u in $unknown) { Write-Output "[FAIL] нечем проверить: $u" }
    foreach ($f in $fail) { Write-Output "[FAIL] $f" }
    exit 2
}
if ($fail.Count) {
    foreach ($f in $fail) { Write-Output "[FAIL] $f" }
    exit 1
}
foreach ($c in $checked) { Write-Output "[PASS] $c" }
Write-Output "[PASS] двигатель цели жив, считает и его эскалация исполняется"
exit 0
