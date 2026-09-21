# Plan step 8: full run on the candidate: go vet, go test, Python plugin tests, candidate build, check-plugin.
# Writes .campaign/results/suite.json (valid for the current cmd/ sources). Exit 1 on any failure.
$ErrorActionPreference = 'Continue'
. (Join-Path $PSScriptRoot 'lib.ps1')
Set-Location $script:Root
$failed = New-Object System.Collections.Generic.List[string]
function Invoke-Part([string]$Name, [scriptblock]$Block) {
    Write-Output ('== ' + $Name)
    & $Block 2>&1 | Select-Object -Last 15 | ForEach-Object { '   ' + $_ }
    if ($LASTEXITCODE -ne 0) { $failed.Add($Name) }
}
$cand = Join-Path $script:Root '.campaign\candidate'
New-Item -ItemType Directory -Force -Path $cand | Out-Null
Invoke-Part 'go vet' { & go -C cmd vet ./... }
Invoke-Part 'go test' { & go -C cmd test ./... -count=1 }
Invoke-Part 'python plugin tests' {
    Push-Location (Join-Path $script:Root '.hermes\plugins\air-worker')
    try { & python -m unittest discover -s tests -t . } finally { Pop-Location }
}
Invoke-Part 'go build candidate' { & go -C cmd build -trimpath -o (Join-Path $cand 'air-worker.exe') . }
Invoke-Part 'check-plugin' { & powershell -NoProfile -ExecutionPolicy Bypass -File tools/check-plugin.ps1 }
$ok = ($failed.Count -eq 0)
Write-Result 'suite' $ok ($failed -join ', ')
if (-not $ok) { Write-Output ('FAILED: ' + ($failed -join ', ')); exit 1 }
Write-Output 'suite OK'
exit 0
