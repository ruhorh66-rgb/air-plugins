# Judge check: the executor may change only Go sources under cmd/ and the campaign plan; anything else in air-worker/ is a scope violation.
. (Join-Path $PSScriptRoot 'lib.ps1')
$repo = (& git -C $script:Root rev-parse --show-toplevel).Trim()
$lines = @(& git -C $repo status --porcelain --untracked-files=all -- air-worker)
$bad = @()
foreach ($l in $lines) {
    if ($l.Length -lt 4) { continue }
    $p = $l.Substring(3).Trim('"').Replace('\', '/')
    if ($p.StartsWith('air-worker/cmd/')) { continue }
    if ($p -eq 'air-worker/docs/campaign-0.10.11-fix/PLAN.md') { continue }
    $bad += $p
}
if ($bad.Count -gt 0) { Write-Output ('NOT CONFIRMED: changes outside cmd/: ' + ($bad -join ', ')); exit 1 }
Write-Output 'OK: only cmd/ and the campaign plan are changed'
exit 0
