[CmdletBinding()]
param(
    [Parameter()][string]$ProjectRoot = (Get-Location).Path,
    [Parameter()][string]$StateFile
)

$ErrorActionPreference = 'Stop'
if (-not $StateFile) { $StateFile = Join-Path $ProjectRoot '.claude-flow\daemon-state.json' }

function Emit([string]$Outcome, [string]$Reason, $State, $ProcessId, $Process, [string]$VerificationMethod) {
    [pscustomobject]@{
        outcome=$Outcome; reason=$Reason; stateFile=$StateFile; stateRunning=$State
        verificationMethod=$VerificationMethod; pid=$ProcessId; processFound=($null -ne $Process); processName=if ($Process) { $Process.Name } else { $null }
    } | ConvertTo-Json -Compress
}

if (-not (Test-Path -LiteralPath $StateFile)) { Emit 'unverifiable' 'daemon-state.json is absent; no claim is made.' $null $null $null 'none'; exit 3 }
try { $state = Get-Content -LiteralPath $StateFile -Raw | ConvertFrom-Json } catch { Emit 'unverifiable' "daemon-state.json cannot be parsed: $($_.Exception.Message)" $null $null $null 'none'; exit 3 }

# Versions differ: accept a top-level pid and the known nested forms, but never invent one.
$pidValue = $state.pid
if (-not $pidValue -and $state.daemon) { $pidValue = $state.daemon.pid }
if (-not $pidValue -and $state.process) { $pidValue = $state.process.pid }
if ($pidValue) {
    try {
        $proc = Get-CimInstance Win32_Process -Filter "ProcessId = $([int]$pidValue)"
    } catch {
        # Sandboxed Windows hosts can deny CIM even though process enumeration is allowed.
        try { $proc = Get-Process -Id ([int]$pidValue) -ErrorAction SilentlyContinue } catch { $proc = $null }
    }
    if ([bool]$state.running -eq [bool]($null -ne $proc)) { Emit 'confirmed' 'State running flag and OS PID agree.' $state.running $pidValue $proc 'pid'; exit 0 }
    Emit 'contradicted' 'State running flag and OS PID disagree.' $state.running $pidValue $proc 'pid'
    exit 2
}

# Current daemon-state.json versions omit PID.  Use a separate OS fact in that case.
try {
    $commandLineProcesses = @(Get-CimInstance Win32_Process -Filter "Name='node.exe'" |
        Where-Object { $_.CommandLine -match 'ruflo|claude-flow' })
} catch {
    Emit 'unverifiable' "State has no PID and command-line process search failed: $($_.Exception.Message)" $state.running $null $null 'commandLine'; exit 3
}

$proc = if ($commandLineProcesses.Count -gt 0) { $commandLineProcesses[0] } else { $null }
if ([bool]$state.running -eq [bool]($null -ne $proc)) { Emit 'confirmed' 'State running flag and matching node command line agree.' $state.running $null $proc 'commandLine'; exit 0 }
Emit 'contradicted' 'State running flag and matching node command line disagree.' $state.running $null $proc 'commandLine'
exit 2
