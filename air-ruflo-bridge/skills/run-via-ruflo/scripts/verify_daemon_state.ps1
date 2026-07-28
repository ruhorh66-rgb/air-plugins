[CmdletBinding()]
param(
    [Parameter()][string]$ProjectRoot = (Get-Location).Path,
    [Parameter()][string]$StateFile
)

$ErrorActionPreference = 'Stop'
if (-not $StateFile) { $StateFile = Join-Path $ProjectRoot '.claude-flow\daemon-state.json' }

function Emit([string]$Outcome, [string]$Reason, $State, $ProcessId, $Process) {
    [pscustomobject]@{
        outcome=$Outcome; reason=$Reason; stateFile=$StateFile; stateRunning=$State
        pid=$ProcessId; processFound=($null -ne $Process); processName=if ($Process) { $Process.Name } else { $null }
    } | ConvertTo-Json -Compress
}

if (-not (Test-Path -LiteralPath $StateFile)) { Emit 'unverifiable' 'daemon-state.json is absent; no claim is made.' $null $null $null; exit 3 }
try { $state = Get-Content -LiteralPath $StateFile -Raw | ConvertFrom-Json } catch { Emit 'unverifiable' "daemon-state.json cannot be parsed: $($_.Exception.Message)" $null $null $null; exit 3 }

# Versions differ: accept a top-level pid and the known nested forms, but never invent one.
$pidValue = $state.pid
if (-not $pidValue -and $state.daemon) { $pidValue = $state.daemon.pid }
if (-not $pidValue -and $state.process) { $pidValue = $state.process.pid }
if (-not $pidValue) { Emit 'unverifiable' 'State has no PID, so the OS process cannot be independently compared.' $state.running $null $null; exit 3 }
try {
    $proc = Get-CimInstance Win32_Process -Filter "ProcessId = $([int]$pidValue)"
} catch {
    # Sandboxed Windows hosts can deny CIM even though process enumeration is allowed.
    try { $proc = Get-Process -Id ([int]$pidValue) -ErrorAction SilentlyContinue } catch { Emit 'unverifiable' "OS process query failed: $($_.Exception.Message)" $state.running $pidValue $null; exit 3 }
}

if ([bool]$state.running -eq [bool]($null -ne $proc)) { Emit 'confirmed' 'State running flag and OS PID agree.' $state.running $pidValue $proc; exit 0 }
Emit 'contradicted' 'State running flag and OS PID disagree.' $state.running $pidValue $proc
exit 2
