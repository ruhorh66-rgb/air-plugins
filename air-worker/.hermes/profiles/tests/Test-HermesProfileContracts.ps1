[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
$checker = Join-Path $repoRoot 'tools\check-hermes-adapter.ps1'
$tracked = @(
    (Join-Path $repoRoot '.hermes\profiles\airworker-hermes-v1-shadow'),
    (Join-Path $repoRoot '.hermes\profiles\airworker-hermes-v1-enforce'),
    $checker
)
function Snapshot {
    $result = @{}
    foreach ($path in $tracked) {
        if (Test-Path $path -PathType Container) {
            foreach ($file in Get-ChildItem $path -Recurse -File) { $result[$file.FullName] = (Get-FileHash $file.FullName -Algorithm SHA256).Hash }
        } else { $result[$path] = (Get-FileHash $path -Algorithm SHA256).Hash }
    }
    return $result
}
$before = Snapshot
& powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $checker -StaticOnly
if ($LASTEXITCODE -ne 0) { throw "static profile contract check failed with exit $LASTEXITCODE" }
$after = Snapshot
if ($before.Count -ne $after.Count) { throw 'checker mutated the package file set' }
foreach ($path in $before.Keys) {
    if (-not $after.ContainsKey($path) -or $before[$path] -ne $after[$path]) { throw "checker mutated $path" }
}
$shadow = Get-Content -Raw (Join-Path $repoRoot '.hermes\profiles\airworker-hermes-v1-shadow\profile-contract.json') | ConvertFrom-Json
$enforce = Get-Content -Raw (Join-Path $repoRoot '.hermes\profiles\airworker-hermes-v1-enforce\profile-contract.json') | ConvertFrom-Json
if ([bool]$shadow.profile.enforcement) { throw 'shadow fixture must not enforce' }
if (-not [bool]$enforce.profile.enforcement) { throw 'enforce fixture must enforce' }
if ($shadow.schema_version -ne $enforce.schema_version -or $shadow.tool.schema_version -ne $enforce.tool.schema_version) { throw 'side-by-side fixtures disagree on schema versions' }
Write-Host 'PASS: profile contracts are valid, side-by-side, and checker is non-mutating'
