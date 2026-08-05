[CmdletBinding()]
param(
    # Тот же канонический корень, что у run_task.ps1: установка в cwd плодила
    # рой на каждый каталог, откуда её случайно запустили.
    [Parameter()][string]$ProjectRoot = 'E:\-4-\ruflo-hive',
    [Parameter()][string]$CliPath,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $ProjectRoot)) { New-Item -ItemType Directory -Path $ProjectRoot -Force | Out-Null }
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).Path

function Find-RufloCli([string]$Root) {
    $candidates = @()
    $candidates += Get-ChildItem -Path (Join-Path $Root '.npm-cache\_npx') -Filter cli.js -Recurse -ErrorAction SilentlyContinue |
        Where-Object { $_.FullName -match '[\\/]claude-flow[\\/]bin[\\/]cli\.js$' }
    $candidates += Get-ChildItem -Path (Join-Path $Root 'node_modules') -Filter cli.js -Recurse -ErrorAction SilentlyContinue |
        Where-Object { $_.FullName -match '[\\/]claude-flow[\\/]bin[\\/]cli\.js$' }
    return $candidates | Select-Object -First 1 -ExpandProperty FullName
}

$existingCli = Find-RufloCli $ProjectRoot
if (-not $Force -and ($existingCli -or (Test-Path -LiteralPath (Join-Path $ProjectRoot '.claude-flow\config.yaml')))) {
    [pscustomobject]@{ outcome='already-installed'; projectRoot=$ProjectRoot; cli=$existingCli; changed=$false } | ConvertTo-Json -Compress
    exit 0
}

if (-not $CliPath) { $CliPath = Find-RufloCli $ProjectRoot }
if (-not $CliPath) {
    throw "Ruflo CLI is not cached under $ProjectRoot. Supply -CliPath to a trusted claude-flow\\bin\\cli.js (for example the pilot cache); this script deliberately does not use the hanging standard installer."
}
if (-not (Test-Path -LiteralPath $CliPath)) { throw "CLI not found: $CliPath" }

# This is the piloted, non-interactive initializer. Do not replace it with the standard ruflo init command.
Push-Location $ProjectRoot
try {
    & node $CliPath init --minimal --no-global --no-signup --no-skills-sh --no-codex-detect
    if ($LASTEXITCODE -ne 0) { throw "ruflo initialization failed with exit code $LASTEXITCODE" }
} finally { Pop-Location }
[pscustomobject]@{ outcome='installed'; projectRoot=$ProjectRoot; cli=(Resolve-Path -LiteralPath $CliPath).Path; changed=$true } | ConvertTo-Json -Compress
