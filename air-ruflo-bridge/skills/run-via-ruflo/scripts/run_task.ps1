# run_task.ps1 — одно окно, один рой.
#
# ПЕРЕПИСАН 05.08.2026 по решению ЛПР: контур не пишет свою оркестрацию поверх
# ruflo (swarm_init/task_create вручную) — используется КАНОНИЧЕСКАЯ команда
# самого движка, `hive-mind spawn --claude -o "<цель>"`. Наша обвязка — это
# только: (1) один и тот же рой для контура, (2) гейт человека перед реальным
# запуском, (3) сканер секретов после.
#
# ОДНО ОКНО. Рой всегда живёт и запускается из ОДНОГО канонического каталога
# ($ProjectRoot). У `hive-mind spawn` нет флага «выполнить в другом каталоге» —
# состояние роя жёстко привязано к cwd вызова. Поэтому реальная работа (другой
# путь, другой проект) не переносит рой физически — целевой путь дописывается
# ТЕКСТОМ в цель, и запущенная Claude Code сессия сама переходит туда первым
# действием. Один рой — один живой процесс — одна очередь целей, как и просил ЛПР.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Objective,
    # Абсолютный путь, где реально лежит код/данные задачи. Рой стоит в
    # $ProjectRoot, но работает здесь — путь вписывается в текст цели.
    [Parameter(Mandatory)][string]$TargetPath,
    [Parameter()][string]$ProjectRoot = 'E:\-4-\ruflo-hive',
    [Parameter()][switch]$AllowForeignHive,
    [Parameter(Mandatory)][string]$CliPath,
    [Parameter(Mandatory)][string]$ReportPath,
    [Parameter()][ValidateSet('I_APPROVE_RUFLO_PLAN')][string]$Approval,
    # Сколько воркеров завести под задачу. ДО 08.08.2026 не передавалось вовсе —
    # движок брал свой дефолт 1 (`-n, --count ... [default: 1]`), поэтому каждый
    # прогон контура шёл ОДНИМ воркером, а 18 накопленных сидели idle с нулём
    # выполненных задач. Значение 5 — из официального примера самого движка
    # (`claude-flow hive-mind spawn -n 5`), не выдумано нами.
    [Parameter()][int]$Workers = 5,
    [Parameter()][ValidateSet('normal','high','critical')][string]$Priority = 'high',
    # autopilot держит агентов в работе, пока не закрыты ВСЕ задачи
    # ("Persistent swarm completion"). Выключается осознанно, не по умолчанию.
    [Parameter()][switch]$NoAutopilot
)

$ErrorActionPreference = 'Stop'

# Движок проверяет доступность Claude Code вызовом `execSync('which claude')`
# (dist/src/commands/hive-mind.js) — ЮНИКСОВЫЙ `which`, которого в Windows нет
# по умолчанию. Проверено 06.08.2026 дважды: ни prepend каталога с claude.exe,
# ни что-либо ещё не помогает, пока самого `which` нет в PATH — движок падает
# в тихую деградацию ("Falling back to displaying instructions"), exit 0,
# реальная сессия не запускается, а отчёт без отдельной проверки врёт "EXECUTED".
# Рабочий рецепт — из проверенного `codex-shim`-шаблона: Git for Windows везёt
# свой which.exe в usr\bin, плюс каталог с настоящим claude.exe для самого
# исполнения (не только для проверки).
$GitBin      = 'C:\Program Files\Git\usr\bin'
$ClaudeExeDir = 'C:\Users\admin_loc\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\bin'
if ((Test-Path -LiteralPath (Join-Path $GitBin 'which.exe')) -and (Test-Path -LiteralPath (Join-Path $ClaudeExeDir 'claude.exe'))) {
    $env:Path = "$GitBin;$ClaudeExeDir;$env:Path"
} else {
    Write-Warning "which.exe ($GitBin) или claude.exe ($ClaudeExeDir) не найдены — реальный запуск, скорее всего, откажет так же, как раньше"
}

if (-not (Test-Path -LiteralPath $CliPath)) { throw "CLI not found: $CliPath" }
if (-not (Test-Path -LiteralPath $TargetPath)) { throw "TargetPath not found: $TargetPath" }
$CanonicalHive = 'E:\-4-\ruflo-hive'
if (-not (Test-Path -LiteralPath $ProjectRoot)) { New-Item -ItemType Directory -Path $ProjectRoot -Force | Out-Null }
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).Path
$TargetPath  = (Resolve-Path -LiteralPath $TargetPath).Path

# ЖЁСТКИЙ ЗАПРЕТ — не отменён этой правкой, один рой остаётся один.
if ($ProjectRoot -ne $CanonicalHive -and -not $AllowForeignHive) {
    [pscustomobject]@{
        outcome='refused'; reason='рой заводится только в каноническом корне'
        requested=$ProjectRoot; canonical=$CanonicalHive
        advice='убрать -ProjectRoot, либо осознанно передать -AllowForeignHive'
    } | ConvertTo-Json -Compress
    exit 5
}

# Замок: живой держатель означает «присоединяйся», а не «подними второй демон».
$hiveSingle = Join-Path $PSScriptRoot 'hive_single.ps1'
if (Test-Path -LiteralPath $hiveSingle) {
    $claim = & $hiveSingle -Action claim -Root $ProjectRoot -Owner "run_task-$PID" 2>$null
    if ($LASTEXITCODE -eq 4) { $claim; exit 4 }
}

$reportDirectory = Split-Path -Parent $ReportPath
if ($reportDirectory -and -not (Test-Path -LiteralPath $reportDirectory)) { New-Item -ItemType Directory -Path $reportDirectory -Force | Out-Null }
$transcript = [System.Collections.Generic.List[string]]::new()

Push-Location $ProjectRoot
try {
    # hive-mind init один раз на канонический рой — идемпотентно по факту наличия .hive-mind
    if (-not (Test-Path -LiteralPath (Join-Path $ProjectRoot '.hive-mind'))) {
        $initOut = & node $CliPath hive-mind init -t hierarchical-mesh 2>&1 | Out-String
        $transcript.Add("## hive-mind init`n~~~text`n$initOut`n~~~")
        if ($LASTEXITCODE -ne 0) { throw "hive-mind init failed ($LASTEXITCODE)" }
    }

    # Проверка регистрации MCP — ОДИН РАЗ на канонический каталог (не на TargetPath:
    # рой всегда запускается из $ProjectRoot, поэтому регистрация нужна только здесь).
    # Без неё `--claude` тихо запускает обычную дорогую сессию БЕЗ mcp__claude-flow__*
    # инструментов — задокументированный отказ (005_Ruflo_Wiki, Пилот 04.08.2026).
    $mcpConfigured = $false
    $mcpJson = Join-Path $ProjectRoot '.mcp.json'
    if (Test-Path -LiteralPath $mcpJson) {
        try {
            $cfg = Get-Content -LiteralPath $mcpJson -Raw | ConvertFrom-Json
            $mcpConfigured = [bool]($cfg.mcpServers.'claude-flow')
        } catch {}
    }
    if (-not $mcpConfigured) {
        $transcript.Add("## MCP registration`nНЕ НАЙДЕНА в $mcpJson. Без неё --claude запустит сессию без инструментов роя.")
    }

    # Цель роя = абсолютный путь работы + сама задача. Рой стоит в $ProjectRoot,
    # спавненная сессия обязана сама перейти в $TargetPath первым действием.
    $fullObjective = "РАБОЧИЙ КАТАЛОГ ЗАДАЧИ: $TargetPath — перейди туда (cd) или используй " +
        "только абсолютные пути на каждую файловую/shell операцию. Эта координирующая сессия " +
        "запущена из $ProjectRoot (канонический рой контура) — файлы этого каталога НЕ ТРОГАТЬ, " +
        "он не относится к задаче. ЗАДАЧА: $Objective"

    $dryOut = & node $CliPath hive-mind spawn --claude -o $fullObjective --dry-run 2>&1 | Out-String
    $planned = "ЧТО БУДЕТ ЗАПУЩЕНО ПРИ -Approval (три шага, не один):`n" +
        "  1. hive-mind spawn -n $Workers        — завести $Workers воркеров под задачу`n" +
        "  2. hive-mind task -d <цель> -p $Priority   — положить задачу в очередь роя`n" +
        "  3. autopilot enable                   — $(if ($NoAutopilot) {'ОТКЛЮЧЕНО флагом -NoAutopilot'} else {'держать агентов до закрытия ВСЕХ задач'})`n" +
        "  4. hive-mind spawn --claude -o <цель> — Queen-сессия, раздаёт работу через MCP`n"
    $transcript.Insert(0, "# Ruflo proposal (canonical hive-mind spawn)`n`nTargetPath: $TargetPath`nProjectRoot: $ProjectRoot`nMCP registered: $mcpConfigured`n`n$planned`n~~~text`n$dryOut`n~~~")
    if ($LASTEXITCODE -ne 0) { throw "hive-mind spawn --dry-run failed ($LASTEXITCODE)" }

    if (-not $Approval) {
        $transcript.Add("## Human gate`nSTOPPED: proposal above must be shown to a human. Re-run only with -Approval I_APPROVE_RUFLO_PLAN. Claude Code was NOT launched.")
        $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
        Write-Output "STOPPED FOR HUMAN APPROVAL. Report: $ReportPath"
        exit 10
    }

    if (-not $mcpConfigured) {
        $transcript.Add("## Refused`nMCP не зарегистрирован для $ProjectRoot — реальный запуск дал бы одну дорогую сессию без роя, не запускаю. Разово: claude mcp add -s project claude-flow -- node `"$CliPath`" mcp start (из $ProjectRoot), затем одобрить в ~/.claude.json.")
        $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
        Write-Output "REFUSED: MCP not registered for $ProjectRoot. Report: $ReportPath"
        exit 6
    }

    # --- Реальный запуск: ТРИ шага, а не один -------------------------------
    # Найдено 08.08.2026 разбором справки самого движка после указания ЛПР
    # «не придумывать, смотреть первоисточник» (ERR-2026-000192). До этой правки
    # вызывался ТОЛЬКО третий шаг, поэтому: воркеров всегда 1 (дефолт движка),
    # очередь задач роя пустая, координатору некого координировать и нечего
    # раздавать. Статистика подтвердила: 18 воркеров, все idle, Completed=0.
    #
    #   1) spawn -n N     — завести воркеров под задачу
    #   2) task -d "цель" — положить задачу В ОЧЕРЕДЬ РОЯ (`Submit tasks to the hive`)
    #   3) spawn --claude — поднять Queen-сессию, которая раздаёт работу через MCP

    $spawnOut = & node $CliPath hive-mind spawn -n $Workers 2>&1 | Out-String
    $transcript.Add("## 1. Spawn workers (-n $Workers)`n~~~text`n$spawnOut`n~~~")
    if ($LASTEXITCODE -ne 0) { throw "hive-mind spawn -n $Workers failed ($LASTEXITCODE)" }

    $taskOut = & node $CliPath hive-mind task -d $fullObjective -p $Priority 2>&1 | Out-String
    $transcript.Add("## 2. Submit task to hive queue (-p $Priority)`n~~~text`n$taskOut`n~~~")
    if ($LASTEXITCODE -ne 0) { throw "hive-mind task failed ($LASTEXITCODE)" }

    if (-not $NoAutopilot) {
        $autoOut = & node $CliPath autopilot enable 2>&1 | Out-String
        $transcript.Add("## 3. Autopilot (persistent completion)`n~~~text`n$autoOut`n~~~")
        if ($LASTEXITCODE -ne 0) { $transcript.Add("ВНИМАНИЕ: autopilot enable вернул $LASTEXITCODE — рой не будет держать задачи до полного закрытия.") }
    }

    # Queen-сессия. Наш внешний гейт (dry-run -> явное решение человека) уже
    # сыграл роль подтверждения — поэтому дефолт движка (--dangerously-skip-permissions)
    # НЕ отключается: отключить его здесь значило бы, что headless-сессия молча
    # виснет на первом же внутреннем запросе разрешения, которого некому одобрить.
    $runOut = & node $CliPath hive-mind spawn --claude -o $fullObjective 2>&1 | Out-String
    $transcript.Add("## Execution`n~~~text`n$runOut`n~~~`nПроверять реальность роя — по логу (`"name`":`"mcp__claude-flow__`"), не по самоотчёту сессии.")

    # НЕ верить exit-коду и факту "команда отработала" — движок сам может тихо
    # не запустить Claude Code и просто напечатать инструкции, при этом exit
    # остаётся 0. Проверено 06.08.2026: отчёт писал "EXECUTED" при реально
    # несостоявшемся запуске (см. runOut ниже). Ищем маркер деградации явно.
    if ($runOut -match 'Claude Code CLI not found' -or $runOut -match 'Falling back to displaying instructions') {
        $transcript.Add("## Execution FAILED (silent degradation)`nДвижок не нашёл claude.exe и не запустил сессию — рой НЕ РАБОТАЛ, несмотря на то что процесс сам завершился без ошибки. См. вывод выше.")
        $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
        Write-Output "EXECUTION FAILED (Claude Code was not actually launched). Report: $ReportPath"
        exit 7
    }
    $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
    Write-Output "EXECUTED. Report: $ReportPath"
} finally {
    Pop-Location

    # Сканер секретов — по TargetPath (где реально менялись файлы), не по ProjectRoot.
    $scanner = Join-Path $PSScriptRoot 'scan_secrets.ps1'
    if ((Test-Path -LiteralPath $scanner) -and $Approval) {
        try {
            $scanJson = & $scanner -Path $TargetPath -ReportPath (Join-Path $ProjectRoot "leaks-$PID.json") 2>&1 | Out-String
            $scan = $scanJson | ConvertFrom-Json
            $verdict = switch ($scan.outcome) {
                'clean'  { "СЕКРЕТОВ НЕ НАЙДЕНО ($($scan.seconds) c, $($scan.verifiedBy))" }
                'leaks'  { "НАЙДЕНА УТЕЧКА: находок $($scan.findings). Смотреть по файлу и строке в $($scan.report)" }
                default  { "ПРОВЕРИТЬ НЕ СМОГ: $($scan.reason). Это НЕ «чисто»." }
            }
            $transcript.Add("## Secret scan`n$verdict`n~~~json`n$scanJson~~~")
            $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
            Write-Output "SECRET SCAN: $verdict"
        } catch {
            $transcript.Add("## Secret scan`nПРОВЕРИТЬ НЕ СМОГ: $($_.Exception.Message). Это НЕ «чисто».")
            $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
            Write-Output "SECRET SCAN: ПРОВЕРИТЬ НЕ СМОГ — $($_.Exception.Message)"
        }
    }

    if (Test-Path -LiteralPath $hiveSingle) {
        & $hiveSingle -Action release -Root $ProjectRoot -Force 2>$null | Out-Null
    }
}
