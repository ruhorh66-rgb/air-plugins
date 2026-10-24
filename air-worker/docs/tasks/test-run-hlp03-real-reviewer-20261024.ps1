$ErrorActionPreference='Stop'
$runner=Join-Path $PSScriptRoot 'run-hlp03-real-reviewer-20261024.ps1'
$tokens=$null; $parseErrors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($runner,[ref]$tokens,[ref]$parseErrors)
if ($parseErrors.Count) { throw ($parseErrors | Out-String) }
foreach ($name in @('Get-AwFootprint','Stop-AwOwnedGo')) {
    $function=$ast.Find({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name},$true)
    if (-not $function) { throw "Missing $name" }
    . ([scriptblock]::Create($function.Extent.Text))
}
$root=Join-Path $env:TEMP ('hlp03-r4-source-test-'+[guid]::NewGuid().ToString('n'))
New-Item -ItemType Directory -Path $root | Out-Null
try {
    [IO.File]::WriteAllText((Join-Path $root 'one.txt'),'abc')
    $complete=Get-AwFootprint $root
    if (-not $complete.complete -or $complete.bytes -ne 3 -or $complete.error_count -ne 0) { throw 'Complete footprint incorrect' }
    function Get-ChildItem { param($LiteralPath,$ErrorAction,$ErrorVariable) $record=[Management.Automation.ErrorRecord]::new([IO.FileNotFoundException]::new('simulated disappearing entry'),'missing',[Management.Automation.ErrorCategory]::ObjectNotFound,$LiteralPath); Set-Variable -Scope 1 -Name $ErrorVariable -Value @($record) }
    $partial=Get-AwFootprint $root
    if ($partial.complete -or $null -ne $partial.bytes -or $partial.error_count -ne 1 -or $partial.errors[0].detail -notmatch 'simulated disappearing entry') { throw "Incomplete footprint was not recorded: $($partial | ConvertTo-Json -Depth 5 -Compress)" }
    Remove-Item Function:Get-ChildItem
    $child=Start-Process -FilePath 'powershell.exe' -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 2' -PassThru -WindowStyle Hidden
    $handle=$child.Handle
    $null=Stop-AwOwnedGo $child
    if (-not $child.HasExited) { throw 'Owned child remains running' }
    'HLP03 R4 deterministic PowerShell source tests passed'
} finally {
    if ($child -and -not $child.HasExited) { & taskkill.exe /PID $child.Id /T /F | Out-Null }
    Remove-Item -LiteralPath $root -Recurse -Force
}
