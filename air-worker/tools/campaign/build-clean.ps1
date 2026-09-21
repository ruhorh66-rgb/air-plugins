# Plan step 10: commit the sources, build both binaries from a clean checkout of that commit, commit the binaries.
# The build revision stamped into the binaries is then the source commit, vcs.modified=false. Run with pwsh 7.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'lib.ps1')
$root = $script:Root
$repo = (& git -C $root rev-parse --show-toplevel).Trim()
Set-Location $repo
$id = @('-c', 'user.name=Aworker', '-c', 'user.email=aworker@srvlm01.local')

# 1. sources and campaign files (bin/ is untouched here; .campaign/ is excluded locally)
& git add -A air-worker
$dirty = & git status --porcelain
if ($dirty) { & git @id commit -q -m 'fix(air-worker): close verification defects D-1..D-4 (sources, tests, campaign files)' }
$src = (& git rev-parse HEAD).Trim()
Write-Output ('source commit: ' + $src)

# 2. clean checkout of exactly that commit, LF
$clean = Join-Path $root '.campaign\clean'
Remove-Item -LiteralPath $clean -Recurse -Force -ErrorAction SilentlyContinue
& git clone -q -c core.autocrlf=false --no-checkout $repo $clean
& git -C $clean -c advice.detachedHead=false checkout -q $src

# 3. build with the flags of the published binaries: -trimpath, CGO off, stripped (tools/check-binary.ps1 documents
#    -ldflags "-s -w"); the tray is a GUI-subsystem program (-H=windowsgui): the published tray has PE subsystem 2.
$env:CGO_ENABLED = '0'
$mod = Join-Path $clean 'air-worker\cmd'
# outputs go OUTSIDE the clone: an untracked file inside it makes the next build stamp vcs.modified=true
$out = Join-Path $root '.campaign\out'
Remove-Item -LiteralPath $out -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $out | Out-Null
& go -C $mod build -trimpath '-ldflags=-s -w' -o (Join-Path $out 'air-worker.exe') .
if ($LASTEXITCODE -ne 0) { throw 'go build air-worker failed' }
& go -C $mod build -trimpath '-ldflags=-s -w -H=windowsgui' -o (Join-Path $out 'air-worker-tray.exe') ./tray
if ($LASTEXITCODE -ne 0) { throw 'go build tray failed' }
Copy-Item -LiteralPath (Join-Path $out 'air-worker.exe') -Destination (Join-Path $root 'bin\air-worker.exe') -Force
Copy-Item -LiteralPath (Join-Path $out 'air-worker-tray.exe') -Destination (Join-Path $root 'bin\air-worker-tray.exe') -Force

# 4. stamp check before committing the binaries
foreach ($n in @('air-worker.exe', 'air-worker-tray.exe')) {
    $info = (& go version -m (Join-Path $root ('bin\' + $n)) | Out-String)
    if ($info -notmatch ('vcs\.revision=' + $src)) { throw ('vcs.revision of ' + $n + ' is not the source commit') }
    if ($info -notmatch 'vcs\.modified=false') { throw ('vcs.modified is not false in ' + $n) }
}
& git add air-worker/bin/air-worker.exe air-worker/bin/air-worker-tray.exe
& git @id commit -q -m ('build(air-worker): package binaries built from a clean checkout of ' + $src.Substring(0, 7))
Write-Output ('binaries committed: ' + (& git rev-parse --short HEAD))
exit 0
