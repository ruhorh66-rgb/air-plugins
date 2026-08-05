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
    [Parameter()][ValidateSet('I_APPROVE_RUFLO_PLAN')][string]$Approval
)

$ErrorActionPreference = 'Stop'
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
    $transcript.Insert(0, "# Ruflo proposal (canonical hive-mind spawn)`n`nTargetPath: $TargetPath`nProjectRoot: $ProjectRoot`nMCP registered: $mcpConfigured`n`n~~~text`n$dryOut`n~~~")
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

    # Реальный запуск: наш внешний гейт (dry-run -> явное решение человека) уже
    # сыграл роль подтверждения — поэтому дефолт движка (--dangerously-skip-permissions)
    # НЕ отключается: отключить его здесь значило бы, что headless-сессия молча
    # виснет на первом же внутреннем запросе разрешения, которого некому одобрить.
    $runOut = & node $CliPath hive-mind spawn --claude -o $fullObjective 2>&1 | Out-String
    $transcript.Add("## Execution`n~~~text`n$runOut`n~~~`nПроверять реальность роя — по логу (`"name`":`"mcp__claude-flow__`"), не по самоотчёту сессии.")
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
