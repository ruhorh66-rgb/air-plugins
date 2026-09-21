# Judge check: the candidate binary must answer "goals" of this product with exit 0 (plan, config and registry agree).
. (Join-Path $PSScriptRoot 'lib.ps1')
if (-not (Test-Path -LiteralPath $script:CandExe)) { Write-Output 'NOT CONFIRMED: candidate binary is not built yet (run-suite step)'; exit 1 }
$out = & $script:CandExe goals -product $script:Root 2>&1 | ForEach-Object { $_.ToString() }
if ($LASTEXITCODE -eq 0) { Write-Output 'OK: goals of the product answer yes'; exit 0 }
Write-Output ('NOT CONFIRMED: goals exit ' + $LASTEXITCODE + ': ' + (($out | Select-Object -First 5) -join ' | '))
exit 1
