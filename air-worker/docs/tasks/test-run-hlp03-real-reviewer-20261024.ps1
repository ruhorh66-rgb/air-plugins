$ErrorActionPreference='Stop'
$runner=Join-Path $PSScriptRoot 'run-hlp03-real-reviewer-20261024.ps1'
$tokens=$null; $parseErrors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($runner,[ref]$tokens,[ref]$parseErrors)
if ($parseErrors.Count) { throw ($parseErrors | Out-String) }
foreach ($name in @('Get-AwFootprint','Get-AwFailureDiagnostics','Save-AwSample','Stop-AwOwnedGo')) {
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
    function Invoke-AwWin32Failure { throw ([ComponentModel.Win32Exception]::new(123,('long detail '*500))) }
    try { Invoke-AwWin32Failure } catch { $caught=$_ }
    $diagnostics=Get-AwFailureDiagnostics $caught 'during' 'cache_enumeration' | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    if ($diagnostics.failure_phase -ne 'during' -or $diagnostics.failure_operation -ne 'cache_enumeration' -or $diagnostics.exception_type -ne 'System.ComponentModel.Win32Exception' -or $diagnostics.native_error_code -ne 123 -or $null -eq $diagnostics.exception_hresult -or -not $diagnostics.error_id) { throw 'Win32 failure metadata lost in JSON' }
    if ($diagnostics.invocation_line_number -lt 1 -or $diagnostics.invocation_offset_in_line -lt 1 -or -not $diagnostics.position_message -or $diagnostics.invocation_line -notmatch 'Win32Exception' -or $diagnostics.script_stack_trace -notmatch 'Invoke-AwWin32Failure') { throw 'Real failure location or stack lost in JSON' }
    if ($diagnostics.error_id.Length -ne 4096 -or $diagnostics.position_message.Length -gt 4096 -or $diagnostics.script_stack_trace.Length -gt 4096 -or $diagnostics.invocation_line.Length -gt 4096) { throw 'Failure detail was not bounded' }
    try { Get-Item -LiteralPath (Join-Path $root 'missing') -ErrorAction Stop } catch { $withoutNative=Get-AwFailureDiagnostics $_ 'after' 'append_sample' | ConvertTo-Json -Depth 5 | ConvertFrom-Json }
    if ($null -ne $withoutNative.native_error_code -or $withoutNative.invocation_command -ne 'Get-Item' -or $withoutNative.failure_phase -ne 'after' -or $withoutNative.failure_operation -ne 'append_sample') { throw 'Unavailable native code, command or failure boundary incorrect' }
    function Get-CimInstance { throw ([ComponentModel.Win32Exception]::new(123,'deterministic disk query fault')) }
    try { Save-AwSample 'after' } catch { $sampleFailure=Get-AwFailureDiagnostics $_ $awFailurePhase $awFailureOperation | ConvertTo-Json -Depth 5 | ConvertFrom-Json }
    Remove-Item Function:Get-CimInstance
    if ($sampleFailure.failure_phase -ne 'after' -or $sampleFailure.failure_operation -ne 'disk_query' -or $sampleFailure.native_error_code -ne 123 -or $sampleFailure.script_stack_trace -notmatch 'Save-AwSample') { throw 'Actual sample failure boundary was not captured' }
    $child=Start-Process -FilePath 'powershell.exe' -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 2' -PassThru -WindowStyle Hidden
    $handle=$child.Handle
    $null=Stop-AwOwnedGo $child
    if (-not $child.HasExited) { throw 'Owned child remains running' }
    'HLP03 R4 deterministic PowerShell source tests passed'
} finally {
    if ($child -and -not $child.HasExited) { & taskkill.exe /PID $child.Id /T /F | Out-Null }
    Remove-Item -LiteralPath $root -Recurse -Force
}
