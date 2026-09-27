param(
    [Parameter(Mandatory=$true)][string]$Repo,
    [Parameter(Mandatory=$true)][string]$Version,
    [string]$Commit = 'HEAD'
)

$ErrorActionPreference = 'Stop'

$repoPath = (Resolve-Path -LiteralPath $Repo).Path
$resolved = (@(& git -C $repoPath rev-parse --verify ($Commit + '^{commit}') 2>$null) -join [Environment]::NewLine).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($resolved)) {
    Write-Output ('FAIL: release commit is not a commit: ' + $Commit)
    exit 2
}

$dirty = @(& git -C $repoPath status --porcelain -- air-worker)
if ($LASTEXITCODE -ne 0) {
    Write-Output 'FAIL: git status failed'
    exit 2
}
if ($dirty.Count -gt 0) {
    Write-Output 'FAIL: air-worker payload is dirty; release identity cannot be proven'
    $dirty | ForEach-Object { Write-Output ('  ' + $_) }
    exit 1
}

$tag = 'air-worker--v' + $Version
$tagExists = (@(& git -C $repoPath tag --list $tag) -join [Environment]::NewLine).Trim()
if (-not [string]::IsNullOrWhiteSpace($tagExists)) {
    $tagCommit = (@(& git -C $repoPath rev-list -n 1 $tag) -join [Environment]::NewLine).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($tagCommit)) {
        Write-Output ('FAIL: existing distribution tag cannot be resolved: ' + $tag)
        exit 2
    }
    if (-not [string]::Equals($tagCommit, $resolved, [StringComparison]::OrdinalIgnoreCase)) {
        Write-Output ('FAIL: distribution identity already published: ' + $tag)
        Write-Output ('  tag commit : ' + $tagCommit)
        Write-Output ('  release    : ' + $resolved)
        Write-Output '  bump version; do not publish changed payload under the existing tag/version'
        exit 1
    }
    Write-Output ('PASS: existing tag ' + $tag + ' already points to release commit ' + $resolved)
    exit 0
}

Write-Output ('PASS: distribution identity is unused: ' + $tag + ' @ ' + $resolved)
exit 0
