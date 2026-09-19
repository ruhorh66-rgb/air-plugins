[CmdletBinding()]
param([switch]$StaticOnly, [switch]$Json)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$profileRoot = Join-Path $repoRoot '.hermes\profiles'
$pluginManifest = Join-Path $repoRoot '.hermes\plugins\air-worker\plugin.yaml'
$profileNames = @('airworker-hermes-v1-shadow', 'airworker-hermes-v1-enforce')
$failures = [System.Collections.Generic.List[string]]::new()
$passes = [System.Collections.Generic.List[string]]::new()

function Pass([string]$Message) { $passes.Add($Message) }
function Fail([string]$Message) { $failures.Add($Message) }
function Read-Utf8NoBom([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { Fail "missing file: $Path"; return $null }
    $bytes = [IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF) { Fail "UTF-8 BOM is forbidden: $Path" }
    try { return [Text.UTF8Encoding]::new($false, $true).GetString($bytes) }
    catch { Fail "not strict UTF-8: $Path"; return $null }
}
function Yaml-Scalar([string]$Text, [string]$Key) {
    $pattern = '(?m)^' + [regex]::Escape($Key) + ':\s*(.+?)\s*$'
    $match = [regex]::Match($Text, $pattern)
    if ($match.Success) { return $match.Groups[1].Value.Trim().Trim('"').Trim("'") }
    return $null
}
function Test-RelativeString([object]$Value, [string]$Location) {
    if ($null -eq $Value) { return }
    if ($Value -is [string]) {
        if ($Value -match '^[A-Za-z]:[\\/]' -or $Value -match '^\\\\' -or $Value -match '^/') { Fail "absolute path forbidden at ${Location}: $Value" }
        return
    }
    if ($Value -is [Collections.IDictionary] -or $Value -is [PSCustomObject]) {
        foreach ($property in $Value.PSObject.Properties) { Test-RelativeString $property.Value "$Location.$($property.Name)" }
        return
    }
    if ($Value -is [Collections.IEnumerable]) {
        $index = 0
        foreach ($item in $Value) { Test-RelativeString $item "$Location[$index]"; $index++ }
    }
}
function Has-Option([string]$Help, [string]$Option) {
    return $Help -match "(?m)(^|[\s,])$([regex]::Escape($Option))([\s,=]|$)"
}

$contracts = @{}
foreach ($name in $profileNames) {
    $startFailures = $failures.Count
    $dir = Join-Path $profileRoot $name
    if (-not (Test-Path -LiteralPath $dir -PathType Container)) { Fail "missing profile package: $name"; continue }
    $forbiddenFiles = @('.env', 'auth.json', 'SOUL.md', 'MEMORY.md', 'USER.md', 'state.db', 'hermes_state.db')
    foreach ($item in Get-ChildItem -LiteralPath $dir -Recurse -Force) {
        if ($forbiddenFiles -contains $item.Name -or $item.Name -match '^(memories|sessions|cron|logs|home|local)$') { Fail "forbidden profile content: $($item.FullName)" }
        if (-not $item.PSIsContainer) { [void](Read-Utf8NoBom $item.FullName) }
    }

    $manifest = Read-Utf8NoBom (Join-Path $dir 'distribution.yaml')
    if ($null -ne $manifest) {
        if ((Yaml-Scalar $manifest 'name') -ne $name) { Fail "$name manifest name mismatch" }
        if ((Yaml-Scalar $manifest 'version') -ne '1.0.0') { Fail "$name manifest version mismatch" }
        if ((Yaml-Scalar $manifest 'hermes_requires') -ne '>=0.21.3') { Fail "$name Hermes version floor mismatch" }
        if ($manifest -notmatch '(?m)^\s*-\s+config\.yaml\s*$' -or $manifest -notmatch '(?m)^\s*-\s+profile-contract\.json\s*$') { Fail "$name distribution_owned mismatch" }
        foreach ($line in $manifest -split "`r?`n") {
            $value = ($line -replace '^\s*[^:#]+:\s*', '').Trim(' ', '"', "'")
            if ($value -match '^[A-Za-z]:[\\/]' -or $value -match '^\\\\' -or $value -match '^/') { Fail "$name manifest has absolute path: $value" }
        }
    }

    $config = Read-Utf8NoBom (Join-Path $dir 'config.yaml')
    if ($null -ne $config -and ($config -replace "`r`n", "`n").Trim() -ne "plugins:`n  enabled:`n    - air-worker") { Fail "$name config is not the minimal air-worker allow-list" }

    $text = Read-Utf8NoBom (Join-Path $dir 'profile-contract.json')
    if ($null -eq $text) { continue }
    try { $contract = $text | ConvertFrom-Json }
    catch { Fail "$name contract is invalid JSON"; continue }
    $contracts[$name] = $contract
    Test-RelativeString $contract $name
    if ($contract.schema_version -ne 'air-worker.hermes-profile/v1' -or $contract.contract_version -ne '1.0.0') { Fail "$name profile contract version mismatch" }
    if ($contract.profile.name -ne $name -or $contract.profile.mode -ne ($name -replace '^airworker-hermes-v1-', '')) { Fail "$name profile identity mismatch" }
    if ([bool]$contract.profile.enforcement -ne $name.EndsWith('-enforce')) { Fail "$name enforcement mismatch" }
    if ($contract.plugin.name -ne 'air-worker' -or $contract.plugin.version -ne '0.10.10') { Fail "$name plugin identity mismatch" }
    if ($contract.tool.name -ne 'air_worker' -or $contract.tool.schema_version -ne 'air-worker.tool/v1') { Fail "$name tool schema mismatch" }
    $caps = $contract.capability_requirements
    $acceptedTransports = @($caps.safe_prompt_transport.accepted | ForEach-Object { [string]$_ } | Sort-Object)
    if (-not [bool]$caps.safe_prompt_transport.required -or ($acceptedTransports -join ',') -ne 'query-file,stdin') { Fail "$name safe transport mismatch" }
    if (-not [bool]$caps.oneshot.required -or -not [bool]$caps.max_turns.required -or -not [bool]$caps.run_budget.required) { Fail "$name bounded run requirements mismatch" }
    if ([bool]$caps.usage_receipt.required -or [bool]$caps.usage_receipt.machine_readable -or [bool]$caps.usage_receipt.combined_with_query_file -or -not [bool]$caps.usage_receipt.deferred_until_supported) { Fail "$name deferred usage receipt contract mismatch" }
    if (-not [bool]$contract.release_policy.fail_closed_on_missing_capability -or $contract.release_policy.activation -ne 'explicit-only') { Fail "$name release policy mismatch" }
    if ($failures.Count -eq $startFailures) { Pass "$name package contract" }
}

$plugin = Read-Utf8NoBom $pluginManifest
if ($null -ne $plugin) {
    if ((Yaml-Scalar $plugin 'name') -ne 'air-worker' -or (Yaml-Scalar $plugin 'version') -ne '0.10.10') { Fail 'canonical plugin manifest is not air-worker 0.10.10' }
    else { Pass 'canonical plugin manifest air-worker 0.10.10' }
}

$hermes = Get-Command hermes -ErrorAction SilentlyContinue
if ($null -ne $hermes) {
    $doctorOutput = & hermes plugins doctor (Split-Path -Parent $pluginManifest) --ci 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { Fail "Hermes plugin doctor failed: $($doctorOutput.Trim())" } else { Pass 'Hermes plugin doctor' }
    if (-not $StaticOnly) {
        $version = (& hermes --version 2>&1 | Select-Object -First 1).ToString().Trim()
        if ($version -notmatch 'v(\d+)\.(\d+)\.(\d+)') { Fail "cannot parse Hermes version: $version" }
        elseif ([version]("{0}.{1}.{2}" -f $Matches[1], $Matches[2], $Matches[3]) -lt [version]'0.21.3') { Fail "Hermes runtime is below 0.21.3: $version" }
        else { Pass 'Hermes runtime satisfies >=0.21.3' }
        $chatHelp = & hermes chat --help 2>&1 | Out-String
        $queryFile = Has-Option $chatHelp '--query-file'
        $stdin = $queryFile -and $chatHelp -match "'\-' reads stdin"
        $oneshot = Has-Option $chatHelp '--oneshot'
        $maxTurns = Has-Option $chatHelp '--max-turns'
        $runBudget = Has-Option $chatHelp '--run-budget'
        $combinedUsage = Has-Option $chatHelp '--usage-file'
        if (-not $queryFile -or -not $stdin) { Fail "Hermes lacks safe query-file/stdin transport ($version)" }
        if (-not $oneshot) { Fail "Hermes lacks chat --oneshot ($version)" }
        if (-not $maxTurns) { Fail "Hermes lacks chat --max-turns ($version)" }
        if (-not $runBudget) { Fail "Hermes lacks chat --run-budget ($version)" }
        if ($queryFile -and $stdin -and $oneshot -and $maxTurns -and $runBudget) { Pass 'Hermes supported bounded one-shot chat capability' }
        $compositionHelp = & hermes chat --query-file - --oneshot --max-turns 1 --run-budget 1 --help 2>&1 | Out-String
        if ($LASTEXITCODE -ne 0 -or -not (Has-Option $compositionHelp '--query-file')) { Fail "Hermes rejects the required combined chat argv ($version)" }
        else { Pass 'Hermes accepts the required combined chat argv' }
        if ($combinedUsage) { Pass 'Hermes optional combined usage receipt is available' }
        else { Pass 'Hermes combined usage receipt is unavailable and non-blocking for contract v1' }
    }
} elseif (-not $StaticOnly) { Fail 'Hermes executable not found; runtime capability and plugin doctor cannot be proven' }
else { Pass 'Hermes unavailable; runtime checks explicitly skipped' }

$result = [ordered]@{ schema_version = 'air-worker.hermes-adapter-check/v1'; outcome = $(if ($failures.Count -eq 0) { 'pass' } else { 'fail' }); static_only = [bool]$StaticOnly; passes = @($passes); failures = @($failures) }
if ($Json) { $result | ConvertTo-Json -Depth 6 }
else {
    foreach ($item in $passes) { Write-Host "PASS: $item" }
    foreach ($item in $failures) { Write-Warning "FAIL: $item" }
    Write-Host ("RESULT: {0} ({1} pass, {2} fail)" -f $result.outcome.ToUpperInvariant(), $passes.Count, $failures.Count)
}
if ($failures.Count -gt 0) { exit 1 }
exit 0
