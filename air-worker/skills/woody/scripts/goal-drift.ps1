#requires -Version 5.1
<#
.SYNOPSIS
    Двигатель цели: ход, не уменьшивший расстояние до цели, продвижением не считается.

.DESCRIPTION
    Восстановлено 13.09.2026 по прямому указанию ЛПР из Goal/Drift Loop ВЕРЫ
    (E:\-8-\air-vera\automation\controller\goal_drift.py, коммит 4eb51ad, 04-05.09.2026).
    Указание дословно: «она оценивала твой ход — двигает процесс к цели или не двигает;
    и ответвление, и сонные ответвления там лечились».

    Главное правило, записанное у ВЕРЫ в трёх местах независимо:

        рост промежуточного числа продвижением не считается,
        если расстояние до цели не уменьшается.

    Отсюда следует то, ради чего механизм и восстанавливается: УХОД В СТОРОНУ И СОН —
    ОДНО И ТО ЖЕ ЯВЛЕНИЕ, а не два. Работа кипит, коммиты идут, страж доволен форматом —
    а расстояние стоит. Молчащий простой даёт ту же картину. Мерить надо расстояние, а не
    признаки усердия: тогда обе беды ловятся одним числом и без догадок о намерении.

    Четыре решения перенесены из ВЕРЫ дословно, потому что каждое там было куплено
    найденным отказом:

    1. ЧИСЛА БЕРУТСЯ ИЗ ФАКТА, А НЕ ИЗ ТЕКСТА. У ВЕРЫ счётчик считался поиском русской
       подстроки в причине и «молча переставал расти», когда причину переписывали.
       Здесь расстояние читается из .goal-verdict.json, который судья кладёт числами.

    2. НЕИЗВЕСТНОЕ НЕ РАВНО НУЛЮ. Метрика, которую нечем измерить, помечается и выводится
       из правил: «ноль здесь читался бы как всё в порядке». Судья с кодом 2 даёт
       расстояние null, а не ноль, и два таких замера подряд — эскалация.

    3. ЭСКАЛАЦИЯ ОСТАНАВЛИВАЕТ, А НЕ СОПРОВОЖДАЕТ. У ВЕРЫ вердикт сперва влиял только на
       отдельных исполнителей, цикл продолжал брать постороннюю работу и кончался PASS —
       «человеческий гейт не имел никакого действия на плоскость управления». Поэтому
       здесь код возврата 2 обязан ОСТАНАВЛИВАТЬ петлю, как код 2 судьи.

    4. СТРОГОСТЬ НЕ СМЯГЧАЕТСЯ. Поздний вердикт не затирает раннего, иначе порядок правил
       в файле менял бы результат.

    Пороги зашиты в механизм — решение ЛПР 12.09.2026 по лестнице и порогам, — но
    перекрываются разделом goal_drift в run-config.json, если продукту нужно иное.

.PARAMETER ProductRoot
    Корень продукта: там лежат .goal-verdict.json, PLAN.md, run-config.json.

.PARAMETER Record
    Дописать замер в историю. Без него — чистое чтение, история не растёт.

.PARAMETER Note
    Чем был ход: заголовок шага, «правка air-woody», «разбор отказа». Пишется в историю,
    чтобы потом было видно, НА ЧТО ушли ходы, не сдвинувшие расстояние.

.OUTPUTS
    Код возврата: 0 — ALLOW, 1 — THROTTLE, 2 — ESCALATE (останавливать), 3 — ЖДЁТ ЛПР
    (работой закрывать нечего, но цель не достигнута: остаток держит человек).
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ProductRoot,
    [switch]$Record,
    [string]$Note = '',
    [switch]$Json,
    [switch]$Quiet
)

$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

$ALLOW = 'ALLOW'; $THROTTLE = 'THROTTLE'; $ESCALATE = 'ESCALATE'; $BLOCKED = 'ЖДЁТ ЛПР'
$SEVERITY = @{ $ALLOW = 0; $BLOCKED = 1; $THROTTLE = 1; $ESCALATE = 2 }

# Строгость только растёт. Порядок правил ниже на исход не влияет.
function Harden([string]$current, [string]$candidate) {
    if ($SEVERITY[$candidate] -gt $SEVERITY[$current]) { return $candidate }
    return $current
}

if (-not (Test-Path -LiteralPath $ProductRoot -PathType Container)) {
    Write-Output "НЕЧЕМ МЕРИТЬ: нет каталога продукта $ProductRoot"
    exit 2
}
$ProductRoot = (Resolve-Path -LiteralPath $ProductRoot).Path

# --- пороги: зашиты, перекрываются продуктом ---------------------------------
$th = @{
    stall_moves_throttle       = 3
    stall_moves_escalate       = 6
    unverifiable_streak_escalate = 2
}
$cfgPath = Join-Path $ProductRoot 'run-config.json'
if (Test-Path -LiteralPath $cfgPath) {
    try {
        $cfg = Get-Content -LiteralPath $cfgPath -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($cfg.goal_drift) {
            foreach ($k in @($th.Keys)) {
                $v = $cfg.goal_drift.$k
                if ($null -ne $v) { $th[$k] = [int]$v }
            }
        }
    } catch { }
}

# --- расстояние по судье: ФАКТ из .goal-verdict.json -------------------------
$judgeDistance = $null
$judgeCode = $null
$limits = @()
$vPath = Join-Path $ProductRoot '.goal-verdict.json'
if (-not (Test-Path -LiteralPath $vPath)) {
    $limits += "машинного вердикта нет ($vPath): расстояние по судье не измеряется. " +
               "Судья продукта должен быть прогнан хотя бы раз."
} else {
    try {
        $v = Get-Content -LiteralPath $vPath -Raw -Encoding UTF8 | ConvertFrom-Json
        $judgeCode = [int]$v.code
        if ($null -ne $v.distance) { $judgeDistance = [int]$v.distance }
        else { $limits += "судья вернул «нечем проверить»: расстояние неизвестно, а не ноль" }
    } catch {
        $limits += "машинный вердикт не разобран: $($_.Exception.Message)"
    }
}

# --- расстояние по плану: открытых шагов, не считая гейтов ЛПР ----------------
# Грамматика та же, что у Read-Plan в woody.ps1: четыре колонки, закрытый шаг помечен
# зачёркнутым номером. Гейты ЛПР из расстояния исключены намеренно: они не закрываются
# работой сессии, и считать их своим долгом значило бы вечно держать ненулевое расстояние.
$planOpen = $null
$planGates = 0
$planPath = Join-Path $ProductRoot 'PLAN.md'
if (-not (Test-Path -LiteralPath $planPath)) {
    $limits += "плана нет ($planPath): расстояние по шагам не измеряется"
} else {
    $planOpen = 0
    foreach ($line in (Get-Content -LiteralPath $planPath -Encoding UTF8)) {
        if ($line -notmatch '^\s*\|') { continue }
        if ($line -match '^\s*\|\s*(~~)?\s*(\d+)\s*(~~)?\s*\|') {
            $closed = [bool]$Matches[1]
            if ($line -match 'гейт') { $planGates++; continue }
            if (-not $closed) { $planOpen++ }
        }
    }
}

# --- сводное расстояние ------------------------------------------------------
# Непроверенное НЕ СВОРАЧИВАЕТСЯ В НОЛЬ и не складывается: если хоть одна часть
# неизвестна, неизвестно и целое. Иначе пропажа судьи выглядела бы как приближение.
$distance = $null
if ($null -ne $judgeDistance -and $null -ne $planOpen) { $distance = $judgeDistance + $planOpen }

# --- история: дописывается, не переписывается --------------------------------
$histDir = Join-Path $ProductRoot '.woody'
$histPath = Join-Path $histDir 'goal-drift.jsonl'
$history = @()
if (Test-Path -LiteralPath $histPath) {
    foreach ($line in (Get-Content -LiteralPath $histPath -Encoding UTF8)) {
        if (-not $line.Trim()) { continue }
        try { $history += ($line | ConvertFrom-Json) } catch { }
    }
}

# --- правила -----------------------------------------------------------------
$verdict = $ALLOW
$reasons = @()
function Fire([string]$rule, [string]$v, [string]$why) {
    $script:verdict = Harden $script:verdict $v
    $script:reasons += [ordered]@{ rule = $rule; verdict = $v; why = $why }
}

# Сколько замеров подряд расстояние не уменьшалось. Считается по истории — по факту
# записанных чисел, а не по тому, что сессия о себе сообщила.
$stall = 0
$unverifiableStreak = 0
$prevKnown = $null
foreach ($h in $history) {
    if ($null -eq $h.distance) { $unverifiableStreak++; continue }
    $unverifiableStreak = 0
    $d = [int]$h.distance
    if ($null -ne $prevKnown) {
        if ($d -lt $prevKnown) { $stall = 0 } else { $stall++ }
    }
    $prevKnown = $d
}
# Текущий замер учитывается наравне с историей — иначе о последнем ходе судили бы
# только на следующем, и ровно он оставался бы безнаказанным.
if ($null -eq $distance) {
    $unverifiableStreak++
} else {
    if ($null -ne $prevKnown) {
        if ($distance -lt $prevKnown) { $stall = 0 } else { $stall++ }
    }
    $unverifiableStreak = 0
}

# НУЛЕВОЕ РАССТОЯНИЕ НЕ МОЖЕТ ЗАСТАИВАТЬСЯ: двигать уже нечего.
#
# Найдено AIR-ENV-002 13.09.2026, вторым заходом и осторожно — она назвала это мелочью
# оформления. Это не мелочь. Расстояние 0 не уменьшается НИКОГДА, а счётчик застоя
# растёт при каждом «не меньше предыдущего», — значит через stall_moves_escalate ходов
# продукт с исчерпанной работой получал бы эскалацию за то, что работа кончилась.
# Ровно тот дефект, от которого её спасли гейтованные факты часом раньше, только
# отложенный на шесть ходов.
#
# И то же самое различает два состояния, которые она справедливо назвала неразличимыми:
# ALLOW при расстоянии 0 и ALLOW при расстоянии 5 с движением. Теперь при нуле вердикт
# СВОЙ, и читающий один вердикт механизм не примет «работать не над чем» за «продолжай».
$workExhausted = ($null -ne $distance -and $distance -eq 0)
if ($workExhausted) {
    if ($null -ne $judgeCode -and $judgeCode -eq 0) {
        $reasons += [ordered]@{ rule = 'WORKER-DRIFT-00'; verdict = $ALLOW
            why = 'цель достигнута: расстояние ноль и судья согласен' }
    } else {
        Fire 'WORKER-DRIFT-00' $BLOCKED ("работой закрывать нечего — расстояние ноль, — но судья цель не " +
            "подтвердил (код $judgeCode). Значит остаток держат гейты ЛПР либо реестр не покрывает того, " +
            "что судья требует. Ни то, ни другое не лечится следующей итерацией.")
    }
}

# WORKER-DRIFT-01 — ходы идут, расстояние стоит. Это и уход в сторону, и сон.
if (-not $workExhausted -and $stall -ge $th.stall_moves_escalate) {
    Fire 'WORKER-DRIFT-01' $ESCALATE ("замеров подряд без уменьшения расстояния — $stall при пороге $($th.stall_moves_escalate): " +
        "работа идёт мимо цели, и продолжать её тем же способом значит платить за то же ещё раз")
} elseif (-not $workExhausted -and $stall -ge $th.stall_moves_throttle) {
    Fire 'WORKER-DRIFT-01' $THROTTLE ("замеров подряд без уменьшения расстояния — $stall при пороге $($th.stall_moves_throttle): " +
        "следующий ход обязан сузить шаг или назвать, почему он не двигает расстояние")
}

# WORKER-DRIFT-02 — «проверить не могу» подряд. Это не «чисто».
if ($unverifiableStreak -ge $th.unverifiable_streak_escalate) {
    Fire 'WORKER-DRIFT-02' $ESCALATE ("расстояние неизмеримо подряд $unverifiableStreak раз при пороге $($th.unverifiable_streak_escalate): " +
        "у механизма нет достижимого состояния «проверено», и работа идёт вслепую")
}

# WORKER-DRIFT-03 — расстояние ВЫРОСЛО. Отдельно от застоя: застой — это ноль движения,
# а рост означает, что сделанное разломало уже закрытое.
if ($null -ne $distance -and $null -ne $prevKnown -and $distance -gt $prevKnown) {
    Fire 'WORKER-DRIFT-03' $THROTTLE ("расстояние выросло с $prevKnown до ${distance}: закрытое ранее перестало быть закрытым")
}

$measure = [ordered]@{
    at                  = (Get-Date).ToString('s')
    distance            = $distance
    judge_distance      = $judgeDistance
    judge_code          = $judgeCode
    plan_open_steps     = $planOpen
    plan_gates          = $planGates
    stall_moves         = $stall
    unverifiable_streak = $unverifiableStreak
    verdict             = $verdict
    note                = $Note
}

if ($Record) {
    if (-not (Test-Path -LiteralPath $histDir)) { New-Item -ItemType Directory -Path $histDir -Force | Out-Null }
    $line = ($measure | ConvertTo-Json -Depth 4 -Compress)
    [IO.File]::AppendAllText($histPath, $line + "`r`n", (New-Object Text.UTF8Encoding $false))
}

if ($Json) {
    ($measure + @{ reasons = $reasons; limits = $limits } | ConvertTo-Json -Depth 5)
} elseif (-not $Quiet) {
    $dText = if ($null -ne $distance) { $distance } else { 'НЕИЗВЕСТНО' }
    Write-Output "вердикт двигателя цели: $verdict"
    Write-Output "  расстояние до цели: $dText (судья $(if ($null -ne $judgeDistance) { $judgeDistance } else { '?' }) + открытых шагов плана $(if ($null -ne $planOpen) { $planOpen } else { '?' }))"
    Write-Output "  замеров подряд без движения: $stall; неизмеримо подряд: $unverifiableStreak"
    foreach ($r in $reasons) { Write-Output "  [$($r.rule)] $($r.verdict): $($r.why)" }
    foreach ($l in $limits) { Write-Output "  ограничение: $l" }
    if (-not $reasons.Count) { Write-Output "  правил не сработало" }
}

# Код 3 у «ждёт ЛПР» отдельный от торможения намеренно. Механизм, читающий один код,
# обязан различать «сузь шаг» и «работы не осталось»: первое лечится следующим ходом,
# второе — только человеком. Склеить их значило бы вернуть ту самую неразличимость.
switch ($verdict) {
    $ESCALATE { exit 2 }
    $BLOCKED  { exit 3 }
    $THROTTLE { exit 1 }
    default   { exit 0 }
}
