[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Task,
    [Parameter()][ValidateRange(1,64)][int]$MaxAgents = 2,
    [Parameter()][string[]]$Roles = @('architect','implementer'),
    [Parameter(Mandatory)][string]$CliPath,
    # ГДЕ ЖИВЁТ РОЙ. Раньше здесь стояло (Get-Location).Path, и это была причина
    # разрастания: состояние ruflo привязано к рабочему каталогу, поэтому каждая
    # сессия из своего cwd заводила СВОЙ рой. За неделю так набралось девять
    # каталогов при нуле живых процессов. Теперь корень один и по умолчанию
    # канонический — рой накапливает опыт в одном месте, а не начинает с нуля.
    [Parameter()][string]$ProjectRoot = 'E:\-4-\ruflo-hive',
    # ГДЕ ИДЁТ РАБОТА. Это РАЗНЫЕ вещи, и их смешение всё и ломало: команда
    # выполняется в каталоге проекта, а состояние роя остаётся в каноническом.
    [Parameter()][string]$WorkDir = (Get-Location).Path,
    [Parameter()][switch]$AllowForeignHive,
    [Parameter(Mandatory)][string]$ReportPath,
    [Parameter()][string]$Command,
    [Parameter()][ValidateSet('I_APPROVE_RUFLO_PLAN')][string]$Approval
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $CliPath)) { throw "CLI not found: $CliPath" }
$CanonicalHive = 'E:\-4-\ruflo-hive'
if (-not (Test-Path -LiteralPath $ProjectRoot)) {
    New-Item -ItemType Directory -Path $ProjectRoot -Force | Out-Null
}
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).Path
$WorkDir = (Resolve-Path -LiteralPath $WorkDir).Path

# ЖЁСТКИЙ ЗАПРЕТ. Отказ громче удобства: молчаливое согласие на чужой корень
# вернуло бы ровно ту россыпь роёв, ради которой всё это и делалось.
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
    if ($LASTEXITCODE -eq 4) {
        $claim; exit 4
    }
}
$reportDirectory = Split-Path -Parent $ReportPath
if ($reportDirectory -and -not (Test-Path -LiteralPath $reportDirectory)) { New-Item -ItemType Directory -Path $reportDirectory -Force | Out-Null }
$transcript = [System.Collections.Generic.List[string]]::new()
function Invoke-RufloTool([string]$Tool, [hashtable]$Params) {
    $json = $Params | ConvertTo-Json -Compress
    $output = & node $CliPath mcp exec --tool $Tool --params $json 2>&1 | Out-String
    $script:transcript.Add("## $Tool`n~~~text`n$output`n~~~")
    if ($LASTEXITCODE -ne 0) { throw "ruflo tool $Tool failed ($LASTEXITCODE)" }
    return $output
}

Push-Location $ProjectRoot
try {
    $swarm = Invoke-RufloTool 'swarm_init' @{ topology='hierarchical'; maxAgents=$MaxAgents }
    $created = foreach ($role in $Roles) {
        Invoke-RufloTool 'task_create' @{ type=$role; description=$Task; priority='normal'; tags=@('human-gated',$role) }
    }
    $plan = [pscustomobject]@{ task=$Task; topology='hierarchical'; maxAgents=$MaxAgents; proposedRoles=$Roles; swarmResult=$swarm; taskResults=$created }
    $transcript.Insert(0, "# Ruflo proposal`n`n~~~json`n$($plan | ConvertTo-Json -Depth 4)`n~~~")

    if (-not $Approval) {
        $transcript.Add("## Human gate`nSTOPPED: proposal above must be shown to a human. Re-run only with -Approval I_APPROVE_RUFLO_PLAN. No terminal_execute was called.")
        $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
        Write-Output "STOPPED FOR HUMAN APPROVAL. Report: $ReportPath"
        exit 10
    }
    if (-not $Command) { throw 'Execution was approved but -Command is missing.' }
    # cwd = каталог РАБОТЫ, а не роя: команда должна выполняться там, где лежит
    # проект, иначе она не найдёт ни кода, ни данных.
    Invoke-RufloTool 'terminal_execute' @{ command=$Command; cwd=$WorkDir } | Out-Null
    $transcript.Add("## Execution`nterminal_execute was invoked only after the exact approval token was supplied. This report does not claim that roles executed independently; inspect Ruflo agent/process evidence before making that claim.")
    $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
    Write-Output "EXECUTED. Report: $ReportPath"
} finally {
    Pop-Location

    # ПРОВЕРКА НА УТЕЧКУ СЕКРЕТОВ. Рой пишет файлы автономно, и никто не смотрит,
    # не утащил ли он токен в рабочее дерево. Прогоняется ВСЕГДА, в том числе
    # после падения: упавший прогон успевает наследить не меньше удачного.
    # Результат дописывается в отчёт и никогда не подменяет исход самого прогона —
    # но и не замалчивается: «проверить не смог» пишется так же громко, как находка.
    $scanner = Join-Path $PSScriptRoot 'scan_secrets.ps1'
    if ((Test-Path -LiteralPath $scanner) -and $Approval) {
        try {
            $scanJson = & $scanner -Path $WorkDir -ReportPath (
                Join-Path $ProjectRoot "leaks-$PID.json") 2>&1 | Out-String
            $scan = $scanJson | ConvertFrom-Json
            $verdict = switch ($scan.outcome) {
                'clean'  { "СЕКРЕТОВ НЕ НАЙДЕНО ($($scan.seconds) c, $($scan.verifiedBy))" }
                'leaks'  { "НАЙДЕНА УТЕЧКА: находок $($scan.findings). Значения не выводятся — смотреть по файлу и строке в $($scan.report)" }
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

    # Замок снимается ВСЕГДА, в том числе после падения: иначе следующая сессия
    # упрётся в мёртвый замок и решит, что рой занят.
    if (Test-Path -LiteralPath $hiveSingle) {
        & $hiveSingle -Action release -Root $ProjectRoot -Force 2>$null | Out-Null
    }
}
