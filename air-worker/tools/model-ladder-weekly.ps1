[CmdletBinding()]
param(
    [string]$Product = (Split-Path -Parent $PSScriptRoot),
    [string]$StateDir = '',
    [switch]$Force
)
$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath($Product)
if (-not $StateDir) { $StateDir = Join-Path $root '.air-worker\model-ladder-weekly' }
$historyDir = Join-Path $StateDir 'history'
$now = [DateTime]::UtcNow
$utf8 = New-Object System.Text.UTF8Encoding($false)

function Finish([object]$doc, [int]$rc) {
    $doc | ConvertTo-Json -Compress -Depth 12
    exit $rc
}
if (-not (Test-Path -LiteralPath (Join-Path $root 'run-config.json') -PathType Leaf)) {
    Finish @{ schema='air-worker.model-ladder-weekly/v1'; status='NOT_PROVEN'; error='run-config.json missing' } 2
}
if (Test-Path -LiteralPath $historyDir) {
    $prev = Get-ChildItem -LiteralPath $historyDir -Filter '*.json' -File |
        Sort-Object Name -Descending | Select-Object -First 1
    if ($prev -and -not $Force) {
        $last = Get-Content -LiteralPath $prev.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
        $lastAt = [DateTime]::Parse($last.observed_utc).ToUniversalTime()
        if ($lastAt -gt $now.AddDays(-7)) {
            Finish @{ schema='air-worker.model-ladder-weekly/v1'; status='NOT_DUE';
                last_observed_utc=$last.observed_utc; last_receipt=$prev.FullName;
                next_due_utc=$lastAt.AddDays(7).ToString('o') } 0
        }
    }
}
$cfgPath = Join-Path $root 'run-config.json'
$hash = (Get-FileHash -LiteralPath $cfgPath -Algorithm SHA256).Hash.ToLowerInvariant()
$gitHead = (& git -C $root rev-parse HEAD 2>$null | Select-Object -First 1)
$aw = Get-Command 'air-worker.exe' -ErrorAction SilentlyContinue
$codex = Get-Command 'codex.cmd' -ErrorAction SilentlyContinue
if (-not $codex) { $codex = Get-Command 'codex.ps1' -ErrorAction SilentlyContinue }
$claude = Get-Command 'claude.exe' -ErrorAction SilentlyContinue
$execOK = $false
$complex = 'UNKNOWN'
$hardest = 'UNKNOWN'
$execRC = 127
if ($aw) {
    $list = & $aw.Source executor list -product $root -json 2>&1
    $execRC = $LASTEXITCODE
    if ($execRC -eq 0) {
        try {
            $doc = ($list | Out-String | ConvertFrom-Json)
            $complex = ($doc.classes | Where-Object class -eq 'complex' | Select-Object -First 1).start_tier
            $hardest = ($doc.classes | Where-Object class -eq 'hardest' | Select-Object -First 1).start_tier
            $execOK = $true
        } catch { $execOK = $false }
    }
}
$codexAuthRC = 127
$oldPreference = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
try {
    if ($codex) {
        $null = & $codex.Source login status 2>&1
        $codexAuthRC = $LASTEXITCODE
    }
    $claudeAuthRC = 127
    $claudeLogged = $false
    if ($claude) {
        $auth = & $claude.Source auth status 2>&1
        $claudeAuthRC = $LASTEXITCODE
        try { $claudeLogged = [bool](($auth | Out-String | ConvertFrom-Json).loggedIn) } catch {}
    }
} finally { $ErrorActionPreference = $oldPreference }
$catalogStatus = 'NOT_PROVEN'
$catalogIds = @()
$catalogScript = Join-Path $PSScriptRoot 'model-catalog-native.ps1'
if ($codexAuthRC -eq 0 -and (Test-Path -LiteralPath $catalogScript -PathType Leaf)) {
    $catalogRaw = & $catalogScript -CodexPath $codex.Source 2>&1
    $catalogRC = $LASTEXITCODE
    if ($catalogRC -eq 0) {
        try {
            $catalogDoc = $catalogRaw | Out-String | ConvertFrom-Json
            $catalogStatus = [string]$catalogDoc.status
            $catalogIds = @($catalogDoc.ids)
        } catch { $catalogStatus = 'NOT_PROVEN' }
    }
}
$ready = ($execOK -and $codexAuthRC -eq 0 -and $claudeLogged -and
    $complex -notmatch 'astra' -and $catalogStatus -eq 'LIST_RETURNED')
$status = if ($ready) { 'PASS_READ_ONLY_PREFLIGHT' } else { 'HOLD_NOT_PROVEN' }
$receipt = [ordered]@{
    schema = 'air-worker.model-ladder-weekly/v1'
    observed_utc = $now.ToString('o')
    product = $root
    status = $status
    config_sha256 = $hash
    git_head = [string]$gitHead
    installed_cli_list_exit = $execRC
    installed_complex_start_tier = [string]$complex
    installed_hardest_start_tier = [string]$hardest
    codex_auth_exit = $codexAuthRC
    claude_auth_exit = $claudeAuthRC
    claude_logged_in = $claudeLogged
    model_catalog = 'NOT_PROVEN'
    completed_model_inference = 'NOT_PROVEN'
    auto_reconfiguration = 'DISABLED_UNTIL_VERIFIED'
    api_billing = 'NOT_USED'
    rollback = 'git checkout of previous source SHA; installed rc.2 unchanged'
    next_due_utc = $now.AddDays(7).ToString('o')
}
New-Item -Path $historyDir -ItemType Directory -Force | Out-Null
$name = $now.ToString('yyyyMMddTHHmmssZ') + '.json'
$target = Join-Path $historyDir $name
$tmp = $target + '.tmp'
[IO.File]::WriteAllText($tmp, ($receipt | ConvertTo-Json -Depth 12), $utf8)
Move-Item -LiteralPath $tmp -Destination $target -ErrorAction Stop
Finish $receipt $(if ($ready) { 0 } else { 2 })
