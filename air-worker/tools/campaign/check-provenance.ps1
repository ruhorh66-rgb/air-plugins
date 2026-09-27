# Judge check: release binaries carry vcs.revision of a commit of this repository, vcs.modified=false,
# and the cmd/ tree of that revision equals the cmd/ tree of HEAD with no uncommitted changes.
. (Join-Path $PSScriptRoot 'lib.ps1')
$repo = (& git -C $script:Root rev-parse --show-toplevel).Trim()
$rev = ''
# PE header facts of the published build: both binaries stripped (no symbol table); main is a console program
# (subsystem 3), the tray is a GUI program (subsystem 2).
function Get-PeInfo([string]$Path) {
    $fs = [System.IO.File]::OpenRead($Path)
    try { $buf = New-Object byte[] 4096; [void]$fs.Read($buf, 0, 4096) } finally { $fs.Close() }
    $pe = [BitConverter]::ToInt32($buf, 0x3c)
    [pscustomobject]@{ Symbols = [BitConverter]::ToUInt32($buf, $pe + 16); Subsystem = [BitConverter]::ToUInt16($buf, $pe + 24 + 68) }
}
foreach ($name in @('air-worker.exe', 'air-worker-tray.exe')) {
    $b = Join-Path $script:Root ('bin\' + $name)
    $pi = Get-PeInfo $b
    $wantSub = 3
    if ($name -eq 'air-worker-tray.exe') { $wantSub = 2 }
    if ($pi.Symbols -ne 0) { Write-Output ('NOT CONFIRMED: ' + $name + ' is not stripped (build with -ldflags "-s -w")'); exit 1 }
    if ($pi.Subsystem -ne $wantSub) { Write-Output ('NOT CONFIRMED: ' + $name + ' has PE subsystem ' + $pi.Subsystem + ', expected ' + $wantSub); exit 1 }
    $info = (& go version -m $b 2>&1 | Out-String)
    $m = [regex]::Match($info, 'vcs\.revision=([0-9a-f]{40})')
    if (-not $m.Success) { Write-Output ('NOT CONFIRMED: no vcs.revision in ' + $name); exit 1 }
    $rev = $m.Groups[1].Value
    if ($info -notmatch 'vcs\.modified=false') { Write-Output ('NOT CONFIRMED: vcs.modified is not false in ' + $name); exit 1 }
    & git -C $repo cat-file -e ($rev + '^{commit}') 2>$null
    if ($LASTEXITCODE -ne 0) { Write-Output ('NOT CONFIRMED: revision ' + $rev.Substring(0, 7) + ' of ' + $name + ' is not in this repository'); exit 1 }
    $t1 = (& git -C $repo rev-parse ($rev + ':air-worker/cmd')).Trim()
    $t2 = (& git -C $repo rev-parse 'HEAD:air-worker/cmd').Trim()
    if ($t1 -ne $t2) { Write-Output ('NOT CONFIRMED: cmd/ tree of ' + $rev.Substring(0, 7) + ' differs from HEAD'); exit 1 }
}
$dirty = & git -C $repo status --porcelain -- air-worker/cmd
if ($dirty) { Write-Output 'NOT CONFIRMED: cmd/ has uncommitted changes'; exit 1 }

# Distribution identity is wider than cmd/: the marketplace payload cannot be republished
# under an already-used version/tag with a different commit, and any dirty air-worker payload
# makes that identity unprovable.
$manifest = Get-Content -LiteralPath (Join-Path $script:Root '.claude-plugin\plugin.json') -Raw | ConvertFrom-Json
$identityGuard = Join-Path $script:Root 'tools\check-distribution-identity.ps1'
& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $identityGuard -Repo $repo -Version ([string]$manifest.version) -Commit HEAD
if ($LASTEXITCODE -ne 0) { Write-Output 'NOT CONFIRMED: distribution identity guard failed'; exit 1 }

Write-Output ('OK: binaries built from a clean checkout of ' + $rev.Substring(0, 7) + ', cmd/ identical to HEAD; distribution identity unused or same-tag/same-commit')
exit 0
