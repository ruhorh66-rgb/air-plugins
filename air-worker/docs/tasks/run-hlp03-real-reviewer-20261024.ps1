$ErrorActionPreference='Stop'
[Console]::OutputEncoding=[Text.Encoding]::UTF8
$awSource='R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker'
$awTask=Join-Path $awSource '.air-worker\learning-loop-20261024'
$awRun=Join-Path $awTask 'hlp03-live-20261024-baseline'
$awUtf8=New-Object Text.UTF8Encoding($false)
function Get-AwSHA([string]$Path) { (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }
function Write-AwJSON([string]$Path,$Value) { [IO.File]::WriteAllText($Path,($Value | ConvertTo-Json -Depth 30),$awUtf8) }
function Get-AwFootprint([string]$Path) {
    $rootItem=Get-Item -LiteralPath $Path
    if (($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "Reparse root: $Path" }
    $measure=Get-ChildItem -LiteralPath $Path -Recurse -File -Force | Measure-Object -Property Length -Sum
    [ordered]@{path=$Path;files=[int64]$measure.Count;bytes=[int64]$measure.Sum}
}
function Save-AwSample([string]$Phase) {
    $disk=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='R:'"
    $cache=Get-AwFootprint $env:GOCACHE
    $temp=Get-AwFootprint $env:GOTMPDIR
    $evidence=Get-AwFootprint $awRun
    $sample=[ordered]@{at_utc=[DateTime]::UtcNow.ToString('o');phase=$Phase;cache=$cache;temp=$temp;evidence=$evidence;cache_temp_bytes=($cache.bytes+$temp.bytes);working_estimate_bytes=2147483648;estimate_exceeded=(($cache.bytes+$temp.bytes) -gt 2147483648);volume_free_bytes=[int64]$disk.FreeSpace;volume_size_bytes=[int64]$disk.Size;disk_red=($disk.FreeSpace -lt 10737418240 -or ($disk.FreeSpace/$disk.Size) -lt 0.10)}
    [IO.File]::AppendAllText((Join-Path $awRun 'resources.jsonl'),(($sample | ConvertTo-Json -Depth 10 -Compress)+[Environment]::NewLine),$awUtf8)
    $sample
}
if (Test-Path -LiteralPath $awRun) { throw "Baseline run path already exists; reconcile it, never overwrite or blindly retry." }
$awFreezePath=Join-Path $awSource 'docs\tasks\HLP03_CORPUS_FREEZE_20261024.json'
$awFreeze=Get-Content -LiteralPath $awFreezePath -Raw -Encoding UTF8 | ConvertFrom-Json
$awCorpus=Join-Path $awSource $awFreeze.corpus_path
$awGold=Join-Path $awSource $awFreeze.gold_path
if ((Get-AwSHA $awCorpus) -ne $awFreeze.corpus_sha256 -or (Get-AwSHA $awGold) -ne $awFreeze.gold_sha256) { throw 'Frozen corpus/gold drift' }
$awBinary=Join-Path $awTask 'air-worker-source.exe'
$awBinarySHA='69343382ba477a74dddd5b86092e31281b45d5d0284d536d4a037fae0aa02021'
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
$awBefore=Save-AwSample 'before'
if ($awBefore.disk_red) { throw 'R: below shared-volume reserve before launch' }
$awStart=[DateTime]::UtcNow
$awArgs=@('test','-v','.','-run','^TestHLP03RealReviewerCorpus$','-count=1','-timeout','1800s')
$awLaunch=[ordered]@{schema='air-worker.hlp03-launch/v1';run_id='HLP03_LIVE_20261024_BASELINE';started_at_utc=$awStart.ToString('o');source_head=(& git -C $awSource rev-parse HEAD);binary_sha256=$awBinarySHA;corpus_sha256=(Get-AwSHA $awCorpus);gold_sha256=(Get-AwSHA $awGold);freeze_sha256=(Get-AwSHA $awFreezePath);protocol_sha256=(Get-AwSHA (Join-Path $awSource 'docs\tasks\HLP03_EVALUATION_PROTOCOL_20261024.json'));harness_sha256=(Get-AwSHA (Join-Path $awSource 'cmd\learn_adapter_live_eval_test.go'));model='gpt-5.6-luna';effort='medium';arguments=$awArgs;working_directory=(Join-Path $awSource 'cmd');output=$env:AIR_WORKER_HLP03_OUTPUT;protected_before=$awProtected}
Write-AwJSON (Join-Path $awRun 'launch.json') $awLaunch
$awGo=Start-Process -FilePath (Get-Command go).Source -ArgumentList $awArgs -WorkingDirectory (Join-Path $awSource 'cmd') -RedirectStandardOutput (Join-Path $awRun 'go-test.stdout.txt') -RedirectStandardError (Join-Path $awRun 'go-test.stderr.txt') -NoNewWindow -PassThru
$awProcessHandle=$awGo.Handle # Keep the process handle so Windows PowerShell retains ExitCode after exit.
Write-AwJSON (Join-Path $awRun 'process.json') ([ordered]@{pid=$awGo.Id;started_at_utc=$awStart.ToString('o')})
$awStopReason=$null
while (-not $awGo.WaitForExit(15000)) {
    $awSample=Save-AwSample 'during'
    if ($awSample.disk_red -or ([DateTime]::UtcNow-$awStart).TotalSeconds -gt 1830) {
        $awStopReason=if ($awSample.disk_red) {'DISK_RED'} else {'OUTER_DEADLINE'}
        & taskkill.exe /PID $awGo.Id /T /F | Out-Null
        break
    }
}
$awGo.WaitForExit()
$awAfter=Save-AwSample 'after'
$awProtectedAfter=[ordered]@{}
$awDrift=@()
foreach ($path in $awProtected.Keys) {
    $awProtectedAfter[$path]=Get-AwSHA $path
    if ($awProtectedAfter[$path] -ne $awProtected[$path]) { $awDrift+=$path }
}
$awResult=[ordered]@{schema='air-worker.hlp03-run-terminal/v1';run_id='HLP03_LIVE_20261024_BASELINE';completed_at_utc=[DateTime]::UtcNow.ToString('o');elapsed_seconds=([DateTime]::UtcNow-$awStart).TotalSeconds;go_exit_code=$awGo.ExitCode;stop_reason=$awStopReason;protected_after=$awProtectedAfter;protected_drift=$awDrift;corpus_unchanged=((Get-AwSHA $awCorpus) -eq $awFreeze.corpus_sha256);gold_unchanged=((Get-AwSHA $awGold) -eq $awFreeze.gold_sha256);resource_after=$awAfter;quality='SCORE_SEPARATELY_AGAINST_FROZEN_GOLD';production_effect='NOT_PROVEN'}
Write-AwJSON (Join-Path $awRun 'terminal.json') $awResult
$awResult | ConvertTo-Json -Depth 15 -Compress
if ($awGo.ExitCode -ne 0 -or $awStopReason -or $awDrift.Count -gt 0) { exit 1 }
exit 0
