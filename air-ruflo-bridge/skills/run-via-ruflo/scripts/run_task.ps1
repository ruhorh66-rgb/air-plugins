[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Task,
    [Parameter()][ValidateRange(1,64)][int]$MaxAgents = 2,
    [Parameter()][string[]]$Roles = @('architect','implementer'),
    [Parameter(Mandatory)][string]$CliPath,
    [Parameter()][string]$ProjectRoot = (Get-Location).Path,
    [Parameter(Mandatory)][string]$ReportPath,
    [Parameter()][string]$Command,
    [Parameter()][ValidateSet('I_APPROVE_RUFLO_PLAN')][string]$Approval
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $CliPath)) { throw "CLI not found: $CliPath" }
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).Path
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
    Invoke-RufloTool 'terminal_execute' @{ command=$Command; cwd=$ProjectRoot } | Out-Null
    $transcript.Add("## Execution`nterminal_execute was invoked only after the exact approval token was supplied. This report does not claim that roles executed independently; inspect Ruflo agent/process evidence before making that claim.")
    $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
    Write-Output "EXECUTED. Report: $ReportPath"
} finally { Pop-Location }
