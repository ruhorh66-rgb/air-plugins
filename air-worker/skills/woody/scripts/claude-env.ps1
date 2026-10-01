function Set-ActiveClaudeConfigDir {
    $dir = [string]$env:CLAUDE_CONFIG_DIR
    if ([string]::IsNullOrWhiteSpace($dir)) {
        $dir = [string][Environment]::GetEnvironmentVariable('CLAUDE_CONFIG_DIR', 'User')
    }
    if ([string]::IsNullOrWhiteSpace($dir) -and $env:USERPROFILE) {
        $dir = Join-Path $env:USERPROFILE '.claude'
    }
    if ([string]::IsNullOrWhiteSpace($dir) -and $env:HOME) {
        $dir = Join-Path $env:HOME '.claude'
    }
    if ([string]::IsNullOrWhiteSpace($dir)) {
        throw 'active Claude profile is unknown; set CLAUDE_CONFIG_DIR'
    }
    $env:CLAUDE_CONFIG_DIR = [IO.Path]::GetFullPath($dir)
}
