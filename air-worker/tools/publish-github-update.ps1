#Requires -Version 5.1
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string]$SourceDir,
    [Parameter(Mandatory=$true)][string]$PluginRoot,
    [Parameter(Mandatory=$true)][string]$Verifier,
    [Parameter(Mandatory=$true)][string]$Version,
    [Parameter(Mandatory=$true)][ValidateSet('stable','prerelease')][string]$Channel,
    [Parameter(Mandatory=$true)][string]$PrivateKey,
    [string]$GitHubRepo = 'ruhorh66-rgb/air-plugins',
    [string]$FeedBranch = 'air-worker-update-feed',
    [string]$PublicKey = '',
    [string]$SigningKeyId = 'air-worker-update-2026-01',
    [switch]$PromoteStable,
    [switch]$VerifyOnly
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($PublicKey)) {
    $PublicKey = Join-Path (Split-Path -Parent $PSScriptRoot) 'docs\update-public-key.b64'
}

function Write-Utf8NoBom([string]$Path, [string]$Text) {
    [IO.File]::WriteAllText($Path, $Text, (New-Object Text.UTF8Encoding($false)))
}

function Invoke-GhJson([string[]]$Arguments) {
    $out = @(& gh @Arguments 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "gh failed: gh $($Arguments -join ' ') :: $($out -join ' | ')" }
    $text = ($out -join [Environment]::NewLine).Trim()
    if ([string]::IsNullOrWhiteSpace($text)) { return $null }
    return $text | ConvertFrom-Json
}

function Get-FeedContentSha([string]$Path) {
    $old = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $out = @(& gh api "repos/$GitHubRepo/contents/$($Path)?ref=$FeedBranch" --jq '.sha' 2>$null)
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $old
    }
    if ($code -ne 0) { return '' }
    return (($out -join '').Trim())
}

function Ensure-FeedBranch {
    $old = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $out = @(& gh api "repos/$GitHubRepo/git/ref/heads/$FeedBranch" --jq '.object.sha' 2>$null)
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $old
    }
    if ($code -eq 0 -and -not [string]::IsNullOrWhiteSpace(($out -join '').Trim())) { return }

    $repo = Invoke-GhJson @('repo','view',$GitHubRepo,'--json','defaultBranchRef')
    $defaultBranch = [string]$repo.defaultBranchRef.name
    if ([string]::IsNullOrWhiteSpace($defaultBranch)) { throw 'default GitHub branch unavailable' }
    $base = @(& gh api "repos/$GitHubRepo/git/ref/heads/$defaultBranch" --jq '.object.sha' 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "cannot resolve default branch: $($base -join ' | ')" }
    $baseSha = (($base -join '').Trim())

    $payload = [ordered]@{ ref = "refs/heads/$FeedBranch"; sha = $baseSha } | ConvertTo-Json
    $tmp = Join-Path $env:TEMP ("air-worker-feed-ref-$PID.json")
    try {
        Write-Utf8NoBom $tmp $payload
        $created = @(& gh api --method POST "repos/$GitHubRepo/git/refs" --input $tmp 2>&1)
        if ($LASTEXITCODE -ne 0) { throw "cannot create feed branch: $($created -join ' | ')" }
    } finally {
        Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
    }
}

function Publish-Manifest([string]$ManifestPath) {
    Ensure-FeedBranch
    $feedPath = "updates/air-worker/$Channel/update-manifest.json"
    $existingSha = Get-FeedContentSha $feedPath
    $body = [ordered]@{
        message = "air-worker update feed: $Channel $Version"
        content = [Convert]::ToBase64String([IO.File]::ReadAllBytes($ManifestPath))
        branch = $FeedBranch
    }
    if (-not [string]::IsNullOrWhiteSpace($existingSha)) { $body.sha = $existingSha }

    $tmp = Join-Path $env:TEMP ("air-worker-feed-put-$PID.json")
    try {
        Write-Utf8NoBom $tmp ($body | ConvertTo-Json -Depth 5)
        $put = @(& gh api --method PUT "repos/$GitHubRepo/contents/$feedPath" --input $tmp 2>&1)
        if ($LASTEXITCODE -ne 0) { throw "feed PUT failed: $($put -join ' | ')" }
    } finally {
        Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
    }

    $remote = Invoke-GhJson @('api',"repos/$GitHubRepo/contents/$($feedPath)?ref=$FeedBranch")
    $remoteBytes = [Convert]::FromBase64String((([string]$remote.content) -replace '\s',''))
    $localBytes = [IO.File]::ReadAllBytes($ManifestPath)
    if ($remoteBytes.Length -ne $localBytes.Length) { throw 'remote manifest length mismatch' }
    for ($i = 0; $i -lt $localBytes.Length; $i++) {
        if ($remoteBytes[$i] -ne $localBytes[$i]) { throw "remote manifest byte mismatch at $i" }
    }
    return $feedPath
}

foreach ($p in @($SourceDir,$PluginRoot,$Verifier,$PrivateKey,$PublicKey)) {
    if (-not (Test-Path -LiteralPath $p)) { throw "required path missing: $p" }
}
if ($Channel -eq 'stable' -and -not $PromoteStable -and -not $VerifyOnly) {
    throw 'stable feed advancement requires explicit -PromoteStable'
}
if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { throw 'gh not found' }
$python = @('python','py') | Where-Object { Get-Command $_ -ErrorAction SilentlyContinue } | Select-Object -First 1
if (-not $python) { throw 'Python not found' }

$Version = $Version.TrimStart('v')
$tag = "air-worker--v$Version"
$cliName = "air-worker-$Version-windows-x64.exe"
$trayName = "air-worker-tray-$Version-windows-x64.exe"
$cliPath = Join-Path $SourceDir $cliName
$trayPath = Join-Path $SourceDir $trayName
foreach ($p in @($cliPath,$trayPath)) {
    if (-not (Test-Path -LiteralPath $p -PathType Leaf)) { throw "release asset missing: $p" }
}

$release = Invoke-GhJson @('release','view',$tag,'--repo',$GitHubRepo,'--json','tagName,isDraft,isPrerelease,publishedAt,assets')
if ([bool]$release.isDraft) { throw "release $tag is a draft" }
if ($Channel -eq 'stable' -and [bool]$release.isPrerelease) { throw 'stable feed cannot reference prerelease' }
if ($Channel -eq 'prerelease' -and -not [bool]$release.isPrerelease) { throw 'prerelease feed must reference a GitHub prerelease' }

$cliHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $cliPath).Hash.ToLowerInvariant()
$trayHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $trayPath).Hash.ToLowerInvariant()
$cliSize = (Get-Item -LiteralPath $cliPath).Length
$traySize = (Get-Item -LiteralPath $trayPath).Length

foreach ($pair in @(@($cliName,$cliHash,$cliSize),@($trayName,$trayHash,$traySize))) {
    $matches = @($release.assets | Where-Object { [string]$_.name -eq [string]$pair[0] })
    if ($matches.Count -ne 1) { throw "GitHub release asset count for $($pair[0]) = $($matches.Count)" }
    if ([int64]$matches[0].size -ne [int64]$pair[2]) { throw "GitHub size mismatch for $($pair[0])" }
    $digest = ([string]$matches[0].digest).ToLowerInvariant()
    if ($digest -ne "sha256:$($pair[1])") { throw "GitHub digest mismatch for $($pair[0]): $digest" }
}

$buildInfo = @(& go version -m $cliPath 2>&1)
if ($LASTEXITCODE -ne 0) { throw 'go version -m failed for release CLI' }
$buildText = $buildInfo -join [Environment]::NewLine
$revisionMatch = [regex]::Match($buildText, 'vcs\.revision=([0-9a-f]{40})')
if (-not $revisionMatch.Success) { throw 'release CLI has no embedded vcs.revision' }
$revision = $revisionMatch.Groups[1].Value
if ($buildText -notmatch 'vcs\.modified=false') { throw 'release CLI was built from modified source' }

$payloadJson = @(& $Verifier update payload -root $PluginRoot -json 2>&1)
if ($LASTEXITCODE -ne 0) { throw "payload snapshot failed: $($payloadJson -join ' | ')" }
$payload = (($payloadJson -join [Environment]::NewLine) | ConvertFrom-Json)
$payloadSha = [string]$payload.payload_sha256
if ($payloadSha -notmatch '^[0-9a-f]{64}$') { throw 'invalid payload snapshot digest' }

$manifestPath = Join-Path $SourceDir 'update-manifest.json'
$manifest = [ordered]@{
    schema = 1
    channel = $Channel
    version = $Version
    published_at = [string]$release.publishedAt
    vcs_revision = $revision
    payload_sha256 = $payloadSha
    cli = [ordered]@{
        name = $cliName
        url = "https://github.com/$GitHubRepo/releases/download/$tag/$cliName"
        sha256 = $cliHash
        size = [int64]$cliSize
    }
    tray = [ordered]@{
        name = $trayName
        url = "https://github.com/$GitHubRepo/releases/download/$tag/$trayName"
        sha256 = $trayHash
        size = [int64]$traySize
    }
    signing_key_id = $SigningKeyId
    manifest_signature = ''
    release_notes_url = "https://github.com/$GitHubRepo/releases/tag/$tag"
}
Write-Utf8NoBom $manifestPath (($manifest | ConvertTo-Json -Depth 7) + [Environment]::NewLine)

& $python (Join-Path $PSScriptRoot 'sign-update-manifest.py') --manifest $manifestPath --private-key $PrivateKey --public-key $PublicKey --key-id $SigningKeyId
if ($LASTEXITCODE -ne 0) { throw 'manifest signing failed' }

$verify = @(& $Verifier update verify -manifest $manifestPath -asset-dir $SourceDir -current 0.0.0 -channel $Channel -json 2>&1)
if ($LASTEXITCODE -ne 0) { throw "client verification failed: $($verify -join ' | ')" }
$verified = (($verify -join [Environment]::NewLine) | ConvertFrom-Json)
if (-not [bool]$verified.verified) { throw 'client did not verify signed manifest' }

$feedPath = ''
if (-not $VerifyOnly) {
    $feedPath = Publish-Manifest $manifestPath

    $oldHome = $env:AIR_WORKER_HOME
    $probeHome = Join-Path $env:TEMP ("air-worker-update-probe-$PID")
    try {
        Remove-Item -LiteralPath $probeHome -Recurse -Force -ErrorAction SilentlyContinue
        $env:AIR_WORKER_HOME = $probeHome
        & $Verifier update channel $Channel | Out-Null
        if ($LASTEXITCODE -ne 0) { throw 'cannot configure clean-client update channel' }
        $probe = @(& $Verifier update check -json 2>&1)
        if ($LASTEXITCODE -ne 0) { throw "clean-client feed check failed: $($probe -join ' | ')" }
        $probeJson = (($probe -join [Environment]::NewLine) | ConvertFrom-Json)
        if ([string]$probeJson.latest_version -ne $Version) {
            throw "clean-client feed latest version=$($probeJson.latest_version), want=$Version"
        }
    } finally {
        $env:AIR_WORKER_HOME = $oldHome
        Remove-Item -LiteralPath $probeHome -Recurse -Force -ErrorAction SilentlyContinue
    }
}

[pscustomobject][ordered]@{
    verified = $true
    published_feed = (-not $VerifyOnly)
    repo = $GitHubRepo
    tag = $tag
    channel = $Channel
    version = $Version
    revision = $revision
    payload_sha256 = $payloadSha
    cli_sha256 = $cliHash
    tray_sha256 = $trayHash
    feed_branch = $FeedBranch
    feed_path = $feedPath
} | ConvertTo-Json -Depth 5
