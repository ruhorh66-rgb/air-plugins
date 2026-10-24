param(
    [string]$CorpusFreeze='docs\tasks\HLP03_CORPUS_FREEZE_20261024.json',
    [string]$OutputBasename='hlp03-live-20261024-baseline',
    [string]$RunID='HLP03_LIVE_20261024_BASELINE',
    [string]$SourceBinary='R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker\.air-worker\learning-loop-20261024\air-worker-source.exe',
    [string]$SourceBinarySHA='69343382ba477a74dddd5b86092e31281b45d5d0284d536d4a037fae0aa02021'
)
$ErrorActionPreference='Stop'
[Console]::OutputEncoding=[Text.Encoding]::UTF8
$awSource='R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker'
$awTask=Join-Path $awSource '.air-worker\learning-loop-20261024'
$awRun=Join-Path $awTask $OutputBasename
$awUtf8=New-Object Text.UTF8Encoding($false)
function Get-AwSHA([string]$Path) { (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }
function Write-AwJSON([string]$Path,$Value) { [IO.File]::WriteAllText($Path,($Value | ConvertTo-Json -Depth 30),$awUtf8) }
function Get-AwRootMetadata([string]$Path) {
    $item=Get-Item -LiteralPath $Path -Force
    [ordered]@{path=$item.FullName;attributes=$item.Attributes.ToString();created_utc=$item.CreationTimeUtc.ToString('o');modified_utc=$item.LastWriteTimeUtc.ToString('o')}
}
function Get-AwFootprint([string]$Path) {
    $rootItem=Get-Item -LiteralPath $Path
    if (($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "Reparse root: $Path" }
    $enumerationErrors=@()
    $measure=Get-ChildItem -LiteralPath $Path -Recurse -File -Force -ErrorAction Continue -ErrorVariable enumerationErrors | Measure-Object -Property Length -Sum
    $details=@($enumerationErrors | Select-Object -First 8 | ForEach-Object { [ordered]@{type=$_.Exception.GetType().FullName;detail=([string]$_.Exception.Message).Substring(0,[Math]::Min(512,([string]$_.Exception.Message).Length))} })
    [ordered]@{path=$Path;complete=($enumerationErrors.Count -eq 0);files=$(if ($enumerationErrors.Count -eq 0) {[int64]$measure.Count} else {$null});bytes=$(if ($enumerationErrors.Count -eq 0) {[int64]$measure.Sum} else {$null});error_count=$enumerationErrors.Count;errors=$details}
}
function Get-AwFailureDiagnostics($Record,[string]$Phase,[string]$Operation) {
    $clip={ param($Value) if ($null -eq $Value) { return $null }; $s=[string]$Value; $s.Substring(0,[Math]::Min(4096,$s.Length)) }
    $invocation=$Record.InvocationInfo
    [ordered]@{
        failure_phase=$Phase;failure_operation=$Operation
        error_id=(& $clip $Record.FullyQualifiedErrorId)
        exception_type=$Record.Exception.GetType().FullName
        exception_hresult=$Record.Exception.HResult
        native_error_code=$(if ($Record.Exception -is [ComponentModel.Win32Exception]) {$Record.Exception.NativeErrorCode} else {$null})
        invocation_command=$(if ($invocation -and $invocation.MyCommand) {& $clip $invocation.MyCommand.Name} else {$null})
        invocation_script_name=$(if ($invocation) {& $clip $invocation.ScriptName} else {$null})
        invocation_line_number=$(if ($invocation) {$invocation.ScriptLineNumber} else {$null})
        invocation_offset_in_line=$(if ($invocation) {$invocation.OffsetInLine} else {$null})
        invocation_line=$(if ($invocation) {& $clip $invocation.Line} else {$null})
        position_message=$(if ($invocation) {& $clip $invocation.PositionMessage} else {$null})
        script_stack_trace=(& $clip $Record.ScriptStackTrace)
    }
}
function Save-AwSample([string]$Phase) {
    $script:awFailurePhase=$Phase
    $script:awFailureOperation='disk_query'
    $disk=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='R:'"
    $script:awFailureOperation='cache_enumeration'
    $cache=Get-AwFootprint $env:GOCACHE
    $script:awFailureOperation='temp_enumeration'
    $temp=Get-AwFootprint $env:GOTMPDIR
    $script:awFailureOperation='evidence_enumeration'
    $evidence=Get-AwFootprint $awRun
    $script:awFailureOperation='sample_assembly'
    $known=($cache.complete -and $temp.complete)
    $sample=[ordered]@{at_utc=[DateTime]::UtcNow.ToString('o');phase=$Phase;complete=($known -and $evidence.complete);cache=$cache;temp=$temp;evidence=$evidence;cache_temp_bytes=$(if ($known) {$cache.bytes+$temp.bytes} else {$null});working_estimate_bytes=2147483648;estimate_exceeded=$(if ($known) {(($cache.bytes+$temp.bytes) -gt 2147483648)} else {$null});volume_free_bytes=[int64]$disk.FreeSpace;volume_size_bytes=[int64]$disk.Size;disk_red=($disk.FreeSpace -lt 10737418240 -or ($disk.FreeSpace/$disk.Size) -lt 0.10)}
    $script:awFailureOperation='append_sample'
    [IO.File]::AppendAllText((Join-Path $awRun 'resources.jsonl'),(($sample | ConvertTo-Json -Depth 10 -Compress)+[Environment]::NewLine),$awUtf8)
    $sample
}
function Stop-AwOwnedGo($Process) {
    $killOutput=$null
    if (-not $Process.HasExited) {
        try { $killOutput=(& taskkill.exe /PID $Process.Id /T /F 2>&1 | Out-String).Trim() } catch { $killOutput=$_.ToString() }
        $Process.WaitForExit() # The owned test has its own bounded deadline if tree termination fails.
    }
    $killOutput
}
if ($OutputBasename -notmatch '^hlp03-[a-z0-9-]+$') { throw 'Invalid output directory basename' }
if ($RunID -notmatch '^HLP03_[A-Z0-9_]+$') { throw 'Invalid run ID' }
if ([IO.Path]::IsPathRooted($CorpusFreeze)) { $awFreezePath=[IO.Path]::GetFullPath($CorpusFreeze) } else { $awFreezePath=[IO.Path]::GetFullPath((Join-Path $awSource $CorpusFreeze)) }
if (-not $awFreezePath.StartsWith((Join-Path $awSource 'docs\tasks')+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) { throw 'Freeze path outside product task directory' }
if (-not [IO.Path]::IsPathRooted($SourceBinary) -or $SourceBinary -notmatch '^[A-Za-z]:\\' -or $SourceBinarySHA -notmatch '^[0-9a-fA-F]{64}$') { throw 'Source binary must have an absolute path and SHA256' }
$awBinary=[IO.Path]::GetFullPath($SourceBinary)
if (-not $awBinary.StartsWith($awTask+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) { throw 'Source binary outside owned task root' }
if (Test-Path -LiteralPath $awRun) { throw 'Run output already exists; never overwrite or retry it.' }
$awFreeze=Get-Content -LiteralPath $awFreezePath -Raw -Encoding UTF8 | ConvertFrom-Json
if ($awFreeze.schema -ne 'air-worker.hlp03-corpus-freeze/v1' -or $awFreeze.case_count -lt 1 -or $awFreeze.case_count -gt 24) { throw 'Invalid frozen case count or schema' }
$awCorpus=[IO.Path]::GetFullPath((Join-Path $awSource $awFreeze.corpus_path))
$awGold=[IO.Path]::GetFullPath((Join-Path $awSource $awFreeze.gold_path))
if (-not $awCorpus.StartsWith((Join-Path $awSource 'cmd\testdata')+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase) -or -not $awGold.StartsWith((Join-Path $awSource 'docs\tasks')+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) { throw 'Frozen inputs outside product' }
if ((Get-AwSHA $awCorpus) -ne $awFreeze.corpus_sha256 -or (Get-AwSHA $awGold) -ne $awFreeze.gold_sha256) { throw 'Frozen corpus/gold drift' }
$awProtocolRelative=if ($awFreeze.protocol_path) {$awFreeze.protocol_path} else {'docs\tasks\HLP03_EVALUATION_PROTOCOL_20261024.json'}
$awProtocolPath=[IO.Path]::GetFullPath((Join-Path $awSource $awProtocolRelative))
if (-not $awProtocolPath.StartsWith((Join-Path $awSource 'docs\tasks')+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) { throw 'Protocol path outside product tasks' }
$awProtocolSHA=Get-AwSHA $awProtocolPath
if ($awFreeze.protocol_sha256 -and $awProtocolSHA -ne $awFreeze.protocol_sha256) { throw 'Frozen protocol drift' }
if ($awFreeze.model -ne 'gpt-5.6-luna' -or $awFreeze.effort -ne 'medium') { throw 'Frozen reviewer configuration differs from pinned production settings' }
$awCases=(Get-Content -LiteralPath $awCorpus -Raw -Encoding UTF8 | ConvertFrom-Json).cases
if ($awCases.Count -ne $awFreeze.case_count) { throw 'Corpus case count differs from freeze' }
$awBinarySHA=$SourceBinarySHA.ToLowerInvariant()
if ((Get-AwSHA $awBinary) -ne $awBinarySHA) { throw 'Source binary drift' }
$awProtected=[ordered]@{
 'C:\Users\admin_loc\AppData\Local\air-worker\bin\air-worker.exe'='339c02469519752a53a4c2e43c4b033a8193e33189c339ffb9e601b087e881ab';
 'R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker\PLAN.md'='d01f86219809aa35161a0a95c7154884c3b1372050ce9bc5bf4024cd12d6dbe4';
 'R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker\plan\release-slices.json'='4914785397401d3dc314e3d77c89a9c8220d91b8a54a2cabdbf866a91a533881';
 'R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker\cmd\go.mod'='ca5b48c57a0a14f48736edae21b0f98dc74e77f0e212f2938416ebb556f75391';
 'R:\-4-\air-worker\worktrees\airworker-0119-n093-worker-b-20261018\air-worker\learning-module.json'='c7d37b35b04acba02ddd2cb291c030c1a5a7a75e71bccf0babf9e7db42a2ab1d'
}
foreach ($path in $awProtected.Keys) { if ((Get-AwSHA $path) -ne $awProtected[$path]) { throw "Protected baseline drift: $path" } }
$env:GOCACHE=Join-Path $awTask 'gocache'
$env:GOTMPDIR=Join-Path $awTask 'qa-temp'
$env:TEMP=$env:GOTMPDIR
$env:TMP=$env:GOTMPDIR
$env:AIR_WORKER_HOOK_STATE_DIR=Join-Path $awTask 'session'
$env:AIR_WORKER_HLP03_LIVE='1'
$env:AIR_WORKER_HLP03_CORPUS=$awCorpus
$env:AIR_WORKER_HLP03_OUTPUT=Join-Path $awRun 'evidence'
$env:AIR_WORKER_HLP03_BINARY=$awBinary
$env:AIR_WORKER_HLP03_BINARY_SHA256=$awBinarySHA
$env:AIR_WORKER_HLP03_MODEL='gpt-5.6-luna'
$env:AIR_WORKER_HLP03_EFFORT='medium'
New-Item -ItemType Directory -Path $awRun | Out-Null
$awRootsBefore=@(Get-AwRootMetadata $awSource; Get-AwRootMetadata $awTask)
$awBefore=Save-AwSample 'before'
if ($awBefore.disk_red) { throw 'R: below shared-volume reserve before launch' }
$awStart=[DateTime]::UtcNow
$awArgs=@('test','-v','.','-run','^TestHLP03RealReviewerCorpus$','-count=1','-timeout','1800s')
$awLaunch=[ordered]@{schema='air-worker.hlp03-launch/v1';run_id=$RunID;started_at_utc=$awStart.ToString('o');source_head=(& git -C $awSource rev-parse HEAD);binary_path=$awBinary;binary_sha256=$awBinarySHA;freeze_path=$awFreezePath;corpus_path=$awCorpus;gold_path=$awGold;case_count=$awCases.Count;runner_sha256=(Get-AwSHA $PSCommandPath);adapter_sha256=(Get-AwSHA (Join-Path $awSource 'cmd\learn_adapter.go'));corpus_sha256=(Get-AwSHA $awCorpus);gold_sha256=(Get-AwSHA $awGold);freeze_sha256=(Get-AwSHA $awFreezePath);protocol_path=$awProtocolPath;protocol_sha256=$awProtocolSHA;harness_sha256=(Get-AwSHA (Join-Path $awSource 'cmd\learn_adapter_live_eval_test.go'));model='gpt-5.6-luna';effort='medium';arguments=$awArgs;working_directory=(Join-Path $awSource 'cmd');output=$env:AIR_WORKER_HLP03_OUTPUT;protected_before=$awProtected;roots_before=$awRootsBefore;outer_deadline_seconds=1830}
Write-AwJSON (Join-Path $awRun 'launch.json') $awLaunch
$awGo=Start-Process -FilePath (Get-Command go).Source -ArgumentList $awArgs -WorkingDirectory (Join-Path $awSource 'cmd') -RedirectStandardOutput (Join-Path $awRun 'go-test.stdout.txt') -RedirectStandardError (Join-Path $awRun 'go-test.stderr.txt') -NoNewWindow -PassThru
$awProcessHandle=$awGo.Handle # Keep the process handle so Windows PowerShell retains ExitCode after exit.
try {
$awFailurePhase='during'
$awFailureOperation='process_record_write'
Write-AwJSON (Join-Path $awRun 'process.json') ([ordered]@{pid=$awGo.Id;started_at_utc=$awStart.ToString('o')})
$awStopReason=$null
$awFailureOperation='wait_exit_observation'
while (-not $awGo.WaitForExit(15000)) {
    $awSample=Save-AwSample 'during'
    $awFailureOperation='wait_exit_observation'
    if ($awSample.disk_red -or ([DateTime]::UtcNow-$awStart).TotalSeconds -gt 1830) {
        $awStopReason=if ($awSample.disk_red) {'DISK_RED'} else {'OUTER_DEADLINE'}
        & taskkill.exe /PID $awGo.Id /T /F | Out-Null
        break
    }
}
$awFailureOperation='wait_exit_observation'
$awGo.WaitForExit()
$awAfter=Save-AwSample 'after'
$awProtectedAfter=[ordered]@{}
$awDrift=@()
$awFailureOperation='protected_hash_readback'
foreach ($path in $awProtected.Keys) {
    $awProtectedAfter[$path]=Get-AwSHA $path
    if ($awProtectedAfter[$path] -ne $awProtected[$path]) { $awDrift+=$path }
}
$awFailureOperation='frozen_hash_readback'
$awResult=[ordered]@{schema='air-worker.hlp03-run-terminal/v1';run_id=$RunID;completed_at_utc=[DateTime]::UtcNow.ToString('o');elapsed_seconds=([DateTime]::UtcNow-$awStart).TotalSeconds;go_exit_code=$awGo.ExitCode;stop_reason=$awStopReason;protected_after=$awProtectedAfter;protected_drift=$awDrift;corpus_unchanged=((Get-AwSHA $awCorpus) -eq $awFreeze.corpus_sha256);gold_unchanged=((Get-AwSHA $awGold) -eq $awFreeze.gold_sha256);binary_unchanged=((Get-AwSHA $awBinary) -eq $awBinarySHA);protocol_unchanged=((Get-AwSHA $awProtocolPath) -eq $awProtocolSHA);harness_unchanged=((Get-AwSHA (Join-Path $awSource 'cmd\learn_adapter_live_eval_test.go')) -eq $awLaunch.harness_sha256);adapter_unchanged=((Get-AwSHA (Join-Path $awSource 'cmd\learn_adapter.go')) -eq $awLaunch.adapter_sha256);runner_unchanged=((Get-AwSHA $PSCommandPath) -eq $awLaunch.runner_sha256);roots_after=@(Get-AwRootMetadata $awSource; Get-AwRootMetadata $awTask);resource_after=$awAfter;quality='SCORE_SEPARATELY_AGAINST_FROZEN_GOLD';production_effect='NOT_PROVEN'}
$awFailureOperation='terminal_serialization_write'
Write-AwJSON (Join-Path $awRun 'terminal.json') $awResult
$awResult | ConvertTo-Json -Depth 15 -Compress
if ($awGo.ExitCode -ne 0 -or $awStopReason -or $awDrift.Count -gt 0 -or -not $awResult.binary_unchanged -or -not $awResult.corpus_unchanged -or -not $awResult.gold_unchanged -or -not $awResult.protocol_unchanged -or -not $awResult.harness_unchanged -or -not $awResult.adapter_unchanged -or -not $awResult.runner_unchanged) { exit 1 }
exit 0
} catch {
    $awFailure=$_
    $awDiagnostics=Get-AwFailureDiagnostics $awFailure $awFailurePhase $awFailureOperation
    $awKillOutput=Stop-AwOwnedGo $awGo
    $failure=[ordered]@{schema='air-worker.hlp03-run-terminal/v1';run_id=$RunID;completed_at_utc=[DateTime]::UtcNow.ToString('o');status='WRAPPER_FAILURE';error_type=$awFailure.Exception.GetType().FullName;error=$awFailure.ToString();go_pid=$awGo.Id;go_exit_code=$awGo.ExitCode;go_finished=$awGo.HasExited;taskkill_output=$awKillOutput;production_effect='NOT_PROVEN'}
    foreach ($key in $awDiagnostics.Keys) { $failure[$key]=$awDiagnostics[$key] }
    Write-AwJSON (Join-Path $awRun 'terminal.json') $failure
    $failure | ConvertTo-Json -Depth 15 -Compress
    exit 1
}
