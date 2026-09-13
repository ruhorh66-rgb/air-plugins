<#
.SYNOPSIS
  Проверка судьи: лестница исполнима на всех ступенях, а не только на нулевой.

.DESCRIPTION
  Основание — две молчаливые поломки, найденные 12.09.2026 разбором:

    ступени 2-3  роняли ValueError при отсутствии команды вызова вместо пропуска
                 с названной причиной; починено;
    ступени 4-5  звали claude.exe по прибитому пути npm-установки, которой на машине
                 НЕ СУЩЕСТВУЕТ — верх лестницы был оборван; починено.

  Обе давали метрику «100 % закрытых на нулевом уровне», и она читалась как дешевизна,
  а означала обрыв: выше нуля дороги нет, и подниматься некуда. Эта проверка нужна
  затем, чтобы такой обрыв больше не выглядел успехом.

  Коды: 0 — все ступени достижимы; 1 — есть оборванная; 2 — проверять нечем.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Continue'

$productRoot = Split-Path -Parent $PSScriptRoot
$scriptsDir = Join-Path $productRoot 'skills\run-worker-task\scripts'
$ladder = Join-Path $scriptsDir 'ladder.py'
$judgeRun = Join-Path $scriptsDir 'claude_judge_run.py'

foreach ($f in @($ladder, $judgeRun)) {
    if (-not (Test-Path -LiteralPath $f -PathType Leaf)) {
        Write-Output "НЕЧЕМ ПРОВЕРИТЬ: нет $f"
        exit 2
    }
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

$ladderText = [System.IO.File]::ReadAllText($ladder, [System.Text.Encoding]::UTF8)
$judgeText  = [System.IO.File]::ReadAllText($judgeRun, [System.Text.Encoding]::UTF8)

# ИНТЕРПРЕТАТОР ИЩЕТСЯ ПО ОТВЕТУ, И ИЩЕТ ЕГО ОБЩАЯ ФУНКЦИЯ. Здесь стоял резолв по имени
# через Get-Command без пробы — последний уцелевший экземпляр дефекта, чинившегося в
# продукте трижды по месту находки. Найдено AIR-ENV-002 13.09.2026: у неё Get-Command
# python отдавал алиас-заглушку магазина, та отвечала «Python was not found» с кодом 9009,
# и рушились ТРИ утверждения сразу — причём первое звучало пустым отказом без причины.
# Полчаса ушло на поиск того, чего не хватает её машине. Не хватало не ей.
. (Join-Path $PSScriptRoot 'lib\resolve-tool.ps1')
$pyInfo = Resolve-ProductPython
$py = if ($pyInfo.Ok) { $pyInfo.Path } else { $null }
if (-not $pyInfo.Ok) { Write-Output ("  интерпретатор не найден: " + $pyInfo.Reason) }

# 1. Ступени 2-3: отсутствие команды даёт ПРОПУСК, а не исключение.
Assert-That 'ступень 2 при отсутствии команды пропускает, а не бросает исключение' {
    $ladderText -match 'openrouter_cmd' -and $ladderText -notmatch 'raise ValueError\([^)]*openrouter_cmd'
}
Assert-That 'ступень 3 при отсутствии команды пропускает, а не бросает исключение' {
    $ladderText -match 'codex_cmd' -and $ladderText -notmatch 'raise ValueError\([^)]*codex_cmd'
}

# 2. Подъём без названной причины остаётся ЗАПРЕЩЁННЫМ. Это не должно «починиться»
#    заодно: молчаливая эскалация дала рою 88 % Opus.
Assert-That 'подъём без названной причины по-прежнему запрещён исключением' {
    $ladderText -match 'escalate\(\) требует непустой reason'
}

# 3. Ступени 4-5: инструмент ищется по ответу, а не прибитым путём.
Assert-That 'путь к claude не прибит в коде' {
    $judgeText -notmatch 'AppData\\\\Roaming\\\\npm\\\\node_modules'
}
Assert-That 'claude резолвится на этой машине' {
    if (-not $py) { throw 'интерпретатор не найден — это нечем проверить' }
    $out = & $py -c "import sys; sys.path.insert(0, r'$scriptsDir'); import claude_judge_run as m; print(m._resolve_claude_exe())" 2>&1 | Out-String
    $resolved = ($out -split "`r?`n" | Where-Object { $_.Trim() } | Select-Object -First 1)
    $resolved -and (Test-Path -LiteralPath $resolved.Trim())
}

# 4. Задание уходит исполнителю не аргументом. Требование 3 нормы AUTO-080,
#    купленное там дважды и здесь в третий раз.
Assert-That 'промпт передаётся через стандартный ввод, а не в командной строке' {
    $judgeText -match 'input=prompt'
}

# 5. Живой прогон 12.09.2026 (шаг 3 плана — не чтение, а прогон) НАШЁЛ то, что
#    статический разбор не видит: код на всех ступенях 1-5 не роняет исключение
#    даже когда инфраструктура под ним лежит (llama-server 8080 и роутер 8090
#    были недоступны, codex не авторизован, claude резолвился, но подпроцессом
#    ответил «Not logged in» — все три легли ЧИСТО, честным «не смог», без
#    исключения). Офлайн-самотест ladder.py (demo(), мокает сеть/очередь)
#    проверяет ровно эти случаи регрессией — быстро, без сети.
Assert-That 'офлайн-самотест ladder.py (demo) проходит целиком' {
    if (-not $py) { throw 'интерпретатор не найден — это нечем проверить' }
    # По коду возврата, не по совпадению кириллической строки: PowerShell ловит
    # stdout python.exe в системной кодовой странице, и «все проверки прошли»
    # приходит битым — demo() либо падает AssertionError (код ≠ 0), либо нет.
    & $py $ladder *> $null
    $LASTEXITCODE -eq 0
}

# 6. Сама очередь (llm-queue) — инфраструктура ступеней 1-3 — должна отвечать
#    независимо от того, поднят ли backend под ней. Если ляжет ОНА САМА (а не
#    llama-server/роутер/codex под ней), это уже обрыв: ступеням 1-3 не во что
#    ставить задание.
Assert-That 'llm-queue dispatcher отвечает на capabilities (есть куда ставить ступени 1-3)' {
    if (-not $py) { throw 'интерпретатор не найден — это нечем проверить' }
    $dispatcherPath = $env:LLM_QUEUE_DISPATCHER
    if (-not $dispatcherPath) { $dispatcherPath = 'E:\-8-\llm-queue\llm-queue\dispatcher.py' }
    if (-not (Test-Path -LiteralPath $dispatcherPath)) { throw "dispatcher.py не найден: $dispatcherPath" }
    $out = & $py $dispatcherPath capabilities --format json 2>&1 | Out-String
    ($out -match '"run-job"') -and ($out -match '"show-job-json"')
}

# 7. Живая картина достижимости ступеней 1-5 — ИНФОРМАЦИОННО, не PASS/FAIL:
#    «инструмент не поднят/не авторизован» — законный исход (2а), а не провал
#    ЭТОЙ проверки. Печатается, чтобы обрыв верха лестницы (как 12.09.2026)
#    больше не читался как «100% закрыто на нулевом уровне» без объяснения.
Write-Output ''
Write-Output 'живая картина достижимости (справочно, в PASS/FAIL не входит):'
if ($py) {
    $routerOut = & $py -c "import sys; sys.path.insert(0, r'$scriptsDir'); import ladder; print(ladder.router_alive(timeout=2))" 2>&1 | Out-String
    Write-Output ("  роутер ступени 2 (127.0.0.1:8090): {0}" -f $routerOut.Trim())
} else {
    Write-Output '  роутер ступени 2: интерпретатор не найден — не проверено'
}
$codexCmd = Get-Command codex -ErrorAction SilentlyContinue
if ($codexCmd) {
    # codex.ps1 (npm-обёртка) пишет предупреждение в stderr и выходит ненулевым
    # кодом при неавторизованной сессии — это НЕ ошибка PowerShell-вызова, а
    # содержательный ответ; ослабляем предпочтение на время вызова и судим по
    # коду, а не по факту записи в error-поток (см. ограничения задания).
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = 'SilentlyContinue'
    & codex login status *> $null
    $loginCode = $LASTEXITCODE
    $ErrorActionPreference = $prevEap
    $loginState = if ($loginCode -eq 0) { 'авторизован' } else { "не авторизован (код $loginCode)" }
    Write-Output ("  codex (ступень 3): CLI найден, {0}" -f $loginState)
} else {
    Write-Output '  codex (ступень 3): CLI не найден в PATH'
}

Write-Output ''
Write-Output ("прошло {0}, провалено {1}" -f $passed, $failed)
if ($failed -gt 0) { exit 1 }
exit 0
