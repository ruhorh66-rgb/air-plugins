#requires -Version 5.1
<#
    Проверка: БИНАРНИК ЕСТЬ, РАБОТАЕТ И СОГЛАСЕН СО СКРИПТОМ.

    Решение ЛПР 13.09.2026: переходим на бинарник, и довод его же — собранный файл никуда
    не ставится, лежит рядом с продуктом и НЕ ТРЕБУЕТ ПОВЫШЕНИЯ ПРАВ. Это снимает тот
    класс гейтов, на котором сегодня встали две соседние площадки.

    ПОЧЕМУ ПРОВЕРЯЕТСЯ РАВЕНСТВО, А НЕ ТОЛЬКО НАЛИЧИЕ. Пока порт неполон, две реализации
    живут рядом: бинарник взял судью и двигатель, петля и планировщик остались скриптами.
    Две реализации одного правила расходятся молча — это ровно та беда, от которой мы весь
    день лечим два источника состояния. Равенство поэтому не доказывается однажды и не
    объявляется в документации, а ПРОВЕРЯЕТСЯ КАЖДЫМ ПРОГОНОМ судьи.

    ПОЧЕМУ НА МАКЕТЕ, А НЕ НА ЖИВОМ ПРОДУКТЕ. Судья продукта зовёт эту проверку; позови
    она судью продукта в ответ — получится рекурсия, и первый же прогон ушёл бы в себя.
    Макет даёт то же сравнение без этого: реестр фактов и план кладутся во временный
    каталог, проверок в нём нет вовсе, и обе реализации считают одно и то же.

    ЧЕТЫРЕ СЛУЧАЯ, и они выбраны не для полноты, а по цене ошибки: зелёный, гейты,
    гейт без причины (третий код) и застой до эскалации. Каждый из них за последние сутки
    был живым отказом, а не выдумкой.
#>
[CmdletBinding()]
param([string]$ProductRoot = '')
if (-not $ProductRoot) { $ProductRoot = Split-Path -Parent $PSScriptRoot }

$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

$fail = @(); $unknown = @(); $ok = @()

$exe = Join-Path $ProductRoot 'bin\air-worker.exe'
if (-not (Test-Path -LiteralPath $exe)) {
    Write-Output "[FAIL] нечем проверить: нет $exe — бинарник не собран"
    Write-Output '[FAIL] собрать: go build -trimpath -ldflags "-s -w" -o bin\air-worker.exe .\cmd'
    exit 2
}
$ok += 'бинарник на месте'

$ver = & $exe version 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Output "[FAIL] нечем проверить: бинарник не отвечает на version (код $LASTEXITCODE)"
    exit 2
}
$ok += "бинарник отвечает: $ver"

$scriptDir = Join-Path $ProductRoot 'skills\woody\scripts'
$judgePs = Join-Path $scriptDir 'judge.ps1'
$driftPs = Join-Path $scriptDir 'goal-drift.ps1'
foreach ($p in @($judgePs, $driftPs)) {
    if (-not (Test-Path -LiteralPath $p)) {
        Write-Output "[FAIL] нечем проверить: нет эталона $p — сравнивать не с чем"
        exit 2
    }
}

# --- макет ------------------------------------------------------------------
$fx = Join-Path ([IO.Path]::GetTempPath()) ("woody-eq-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Path (Join-Path $fx 'goal') -Force | Out-Null
try {
    # Проверок в макете нет намеренно: сравниваем СЧЁТ ФАКТОВ И ПРАВИЛА, а не умение
    # запускать чужие скрипты. Последнее проверяется живым продуктом каждым прогоном.
    '{"judge":{"checks":[],"checklist":"goal/checklist.json","min_facts":7}}' |
        Set-Content (Join-Path $fx 'run-config.json') -Encoding UTF8
    ("| № | Шаг | Ступень | Судья |`n|---|---|---|---|`n" +
     "| ~~1~~ | сделано | ``script`` | ведущая |`n" +
     "| 2 | предстоит | ``script`` | ведущая |`n" +
     "| 3 | выпуск | — | гейт: ЛПР |") | Set-Content (Join-Path $fx 'PLAN.md') -Encoding UTF8

    function Set-Facts([int]$closed, [int]$gated, [switch]$GateWithoutReason) {
        $items = @()
        for ($i = 1; $i -le $closed; $i++) { $items += @{ id = ('c{0:d2}' -f $i); status = 'completed' } }
        for ($i = 1; $i -le $gated; $i++) {
            $it = @{ id = ('g{0:d2}' -f $i); status = 'gated' }
            if (-not ($GateWithoutReason -and $i -eq 1)) { $it.awaits = 'повышение прав' }
            $items += $it
        }
        @{ items = $items } | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $fx 'goal\checklist.json') -Encoding UTF8
    }

    function Compare-Judge([string]$case) {
        $psOut = (& powershell -NoProfile -ExecutionPolicy Bypass -File $judgePs -ProductRoot $fx 2>&1 | Out-String).Trim()
        $psCode = $LASTEXITCODE
        $psVerdict = $null
        try { $psVerdict = Get-Content (Join-Path $fx '.goal-verdict.json') -Raw -Encoding UTF8 | ConvertFrom-Json } catch { }
        $goOut = (& $exe judge -product $fx 2>&1 | Out-String).Trim()
        $goCode = $LASTEXITCODE
        $goVerdict = $null
        try { $goVerdict = Get-Content (Join-Path $fx '.goal-verdict.json') -Raw -Encoding UTF8 | ConvertFrom-Json } catch { }
        if ($psCode -ne $goCode) {
            $script:fail += "судья, $case : код скрипта $psCode, код бинарника $goCode"
            return
        }
        if ($psOut -ne $goOut) {
            $script:fail += "судья, $case : текст расходится. скрипт «$psOut»; бинарник «$goOut»"
            return
        }
        # СРАВНИВАЮТСЯ И ЧИСЛА ВЕРДИКТА, А НЕ ТОЛЬКО КОД. Найдено AIR-ENV-002 13.09.2026:
        # проверка сверяла один код возврата, а утверждение делалось про совпадение
        # формата файлов — верный замер, вывод шире основания. Тот же класс, на котором
        # она в тот же день поймала собственный гейт.
        #
        # Сравниваются ЗНАЧЕНИЯ, а не байты: сериализация у двух реализаций разная
        # намеренно (отступы ConvertTo-Json против отступов Go), и требовать побайтового
        # равенства значило бы подгонять одну реализацию под причуды форматирования
        # другой. Равны обязаны быть числа и вердикт, а не расстановка пробелов.
        if (-not $psVerdict -or -not $goVerdict) {
            $script:fail += "судья, $case : машинный вердикт не прочитан у одной из реализаций"
            return
        }
        foreach ($k in @('code','distance','checks_passed','checks_failed','checks_unknown','facts_closed','facts_gated','facts_required','verdict_text')) {
            if ([string]$psVerdict.$k -ne [string]$goVerdict.$k) {
                $script:fail += "судья, $case : поле $k расходится — скрипт «$($psVerdict.$k)», бинарник «$($goVerdict.$k)»"
            }
        }
        # Набор полей тоже сверяется: поле, появившееся у одной реализации и не у другой,
        # и есть начало молчаливого расхождения.
        $psKeys = @($psVerdict.PSObject.Properties.Name | Sort-Object) -join ','
        $goKeys = @($goVerdict.PSObject.Properties.Name | Sort-Object) -join ','
        if ($psKeys -ne $goKeys) {
            $script:fail += "судья, $case : наборы полей вердикта различаются — скрипт «$psKeys», бинарник «$goKeys»"
        }
        if ($script:fail.Count -eq 0 -or -not ($script:fail[-1] -like "судья, $case*")) {
            $script:ok += "судья согласен, $case : код, текст и все числа вердикта"
        }
    }

    Set-Facts -closed 7 -gated 0;  Compare-Judge 'всё закрыто'
    Set-Facts -closed 4 -gated 3;  Compare-Judge 'три факта ждут ЛПР'
    Set-Facts -closed 4 -gated 3 -GateWithoutReason; Compare-Judge 'гейт без названной причины'

    # --- двигатель: вся лестница застоя -------------------------------------
    Set-Facts -closed 4 -gated 0
    & powershell -NoProfile -ExecutionPolicy Bypass -File $judgePs -ProductRoot $fx *> $null
    New-Item -ItemType Directory -Path (Join-Path $fx '.woody') -Force | Out-Null
    $hist = Join-Path $fx '.woody\goal-drift.jsonl'
    # Расстояние макета: судья 3 непокрытых факта + 1 открытый шаг плана = 4.
    $mismatch = 0
    foreach ($n in 0..7) {
        Remove-Item -LiteralPath $hist -Force -ErrorAction SilentlyContinue
        if ($n -gt 0) { 1..$n | ForEach-Object { Add-Content -LiteralPath $hist -Value '{"distance":4}' -Encoding UTF8 } }
        & powershell -NoProfile -ExecutionPolicy Bypass -File $driftPs -ProductRoot $fx -Quiet *> $null
        $psCode = $LASTEXITCODE
        & $exe drift -product $fx -quiet *> $null
        $goCode = $LASTEXITCODE
        if ($psCode -ne $goCode) {
            $fail += "двигатель, история $n замеров: скрипт $psCode, бинарник $goCode"
            $mismatch++
        }
    }
    if ($mismatch -eq 0) { $ok += 'двигатель согласен на всей лестнице застоя (8 точек: ALLOW, торможение, эскалация)' }
}
finally {
    Remove-Item -LiteralPath $fx -Recurse -Force -ErrorAction SilentlyContinue
}

if ($ok.Count -eq 0 -and $fail.Count -eq 0) { Write-Output '[FAIL] нечем проверить: ни одно сравнение не собралось'; exit 2 }
if ($unknown.Count) { foreach ($u in $unknown) { Write-Output "[FAIL] нечем проверить: $u" }; exit 2 }
if ($fail.Count) { foreach ($f in $fail) { Write-Output "[FAIL] $f" }; exit 1 }
foreach ($o in $ok) { Write-Output "[PASS] $o" }
Write-Output '[PASS] бинарник собран, отвечает и согласен со скриптом на всех проверенных случаях'
exit 0
