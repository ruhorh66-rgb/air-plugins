# Judge check: reads .campaign/results/<name>.json written by a plan step. Positional argument: the name.
# Never returns 2: exit 1 with "NOT CONFIRMED" is a normal red, exit 2 would stop the loop.
param([Parameter(Position = 0)][string]$Name)
. (Join-Path $PSScriptRoot 'lib.ps1')
$f = Join-Path $script:ResDir ($Name + '.json')
if (-not (Test-Path -LiteralPath $f)) { Write-Output ('NOT CONFIRMED: ' + $Name + ' has not been run yet'); exit 1 }
$r = Get-Content -Raw -LiteralPath $f | ConvertFrom-Json
if ($r.src -ne (Get-SrcHash)) { Write-Output ('NOT CONFIRMED: ' + $Name + ' result is stale, sources changed after it'); exit 1 }
if (-not $r.ok) { Write-Output ('NOT CONFIRMED: ' + $Name + ' failed: ' + $r.detail); exit 1 }
Write-Output ('OK: ' + $Name + ' ' + $r.at)
exit 0
