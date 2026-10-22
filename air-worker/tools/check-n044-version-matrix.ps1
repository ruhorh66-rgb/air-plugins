param(
    [Parameter(Mandatory=$true)][string]$LegacyRepo,
    [Parameter(Mandatory=$true)][string]$CurrentRepo,
    [Parameter(Mandatory=$true)][string]$LegacySHA,
    [Parameter(Mandatory=$true)][string]$CurrentSHA,
    [Parameter(Mandatory=$true)][string]$OutputDir
)
$ErrorActionPreference='Stop'
Set-StrictMode -Version Latest

# Machine-verifiable, source-bound dual-version QA gate.
# Historical signed R03/R04 positives MUST run on the exact 0.11.9/v0.1.0
# source; current 0.11.10/v0.1.1 MUST reject legacy release execution.
# No source, PLAN, LEARN, production binary or services are mutated.
function Inspect-Root([string]$root,[string]$wantSHA,[string]$version,[string]$module) {
    $actual=(& git -C $root rev-parse HEAD).Trim()
    if($LASTEXITCODE -ne 0 -or $actual -ne $wantSHA) {
        throw "Source SHA mismatch: $root actual=$actual required=$wantSHA"
    }
    $dirty=(& git -C $root status --porcelain | Out-String).Trim()
    if($LASTEXITCODE -ne 0 -or $dirty) {throw "Source worktree is not clean: $root"}
    $dir=Join-Path $root 'air-worker\cmd'
    $mod=Join-Path $dir 'go.mod'
    $main=Join-Path $dir 'main.go'
    if(!(Test-Path -LiteralPath $mod) -or !(Test-Path -LiteralPath $main)) {throw "Go CLI source unavailable: $root"}
    $text=[IO.File]::ReadAllText($mod)
    $goVersion=[IO.File]::ReadAllText($main)
    if(!$text.Contains("github.com/ruhorh66-rgb/air-modules $module") -or
       !$goVersion.Contains('version = "'+$version+'"')) {
        throw "Binary/module source identity mismatch: $root"
    }
    return @{sha=$actual;root=$root;dir=$dir;plan=Join-Path $root 'air-worker\plan\release-slices.json'}
}
function Run-SourceTests([hashtable]$item,[string]$lane,[string[]]$names) {
    $tmp=Join-Path $OutputDir ('gotmp-'+$lane)
    New-Item -ItemType Directory -Path $tmp -Force | Out-Null
    $env:GOTMPDIR=$tmp
    $env:TEMP=$tmp
    $env:TMP=$tmp
    $log=Join-Path $OutputDir ("N044_"+$lane+"_QA.txt")
    if(Test-Path -LiteralPath $log){throw "Existing immutable matrix log: $log"}
    $pattern='^('+($names -join '|')+')$'
    Push-Location $item.dir
    try {
       $line='go test -p 1 -count=1 -parallel=2 -run "'+$pattern+'" -v . > "'+$log+'" 2>&1'
       & cmd.exe /d /s /c $line
       $exitCode=$LASTEXITCODE
    } finally {
       Pop-Location
    }
    $content=[IO.File]::ReadAllText($log)
    $missing=@()
    foreach($name in $names){
       if($content -notmatch ('(?m)^--- PASS: '+[regex]::Escape($name)+' ')){$missing+= $name}
    }
    $fails=[regex]::Matches($content,'(?m)^--- FAIL: ').Count
    $skips=[regex]::Matches($content,'(?m)^--- SKIP: ').Count
    $pass=($exitCode -eq 0 -and $missing.Count -eq 0 -and $fails -eq 0 -and $skips -eq 0)
    return [ordered]@{
       lane=$lane;sha=$item.sha;go_exit=$exitCode;
       expected_tests=$names; missing_pass=$missing;failures=$fails;skips=$skips;
       success=$pass;log_path=$log;log_sha256=(Get-FileHash -LiteralPath $log -Algorithm SHA256).Hash.ToLowerInvariant()
    }
}
$legacyTests=@(
'TestN042HistoricalR02FixtureIsPureAndOnlyRewindsTestState',
'TestR03AuthenticatedHistoricalReviewAdvancesOnlyReleaseCheckpoint',
'TestR03UnattestedTestReviewerCannotPromoteOrRetry',
'TestR03NativeRunnerReusesAuthenticatedReviewedCheckpointExactlyOnce',
'TestR03ModuleRollbackAfterHistoricalCheckpointCannotCreditR04Use',
'TestR04NativeMeasuredEffectExactOneAndReplayNoNewReviewer',
'TestR04RefusesUnmeasuredUseAndModuleRollback',
'TestR04RejectsRealUseRecordedBeforeSignedReleaseOpen'
)
$currentTests=@(
'TestN044CurrentConsumerRejectsHistoricalR03R04ReleaseWork',
'TestR03AuthenticatedHistoricalReviewAdvancesOnlyReleaseCheckpoint',
'TestR03UnattestedTestReviewerCannotPromoteOrRetry',
'TestR03NativeRunnerReusesAuthenticatedReviewedCheckpointExactlyOnce',
'TestR03ModuleRollbackAfterHistoricalCheckpointCannotCreditR04Use',
'TestCmdLoopRoutesR03OnlyAfterNativeR02StatusTransition',
'TestR04NativeMeasuredEffectExactOneAndReplayNoNewReviewer',
'TestR04RefusesUnmeasuredUseAndModuleRollback',
'TestR04RejectsRealUseRecordedBeforeSignedReleaseOpen',
'TestN093ProductionAdapterRejectsUnsignedSyntheticMachineCapsule',
'TestN093ModuleReviewedCapsuleActuallyRunsNativeDeferredUse',
'TestN093NativeReviewerAbsentSignedAttestationCannotClaimNewUse'
)
$origTmp=$env:TEMP;$origTemp=$env:TMP;$origGoTmp=$env:GOTMPDIR
try {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
    $legacy=Inspect-Root $LegacyRepo $LegacySHA '0.11.9' 'v0.1.0'
    $current=Inspect-Root $CurrentRepo $CurrentSHA '0.11.10' 'v0.1.1'
    $legacyPlanSHA=(Get-FileHash -LiteralPath $legacy.plan -Algorithm SHA256).Hash
    $currentPlanSHA=(Get-FileHash -LiteralPath $current.plan -Algorithm SHA256).Hash
    $old=Run-SourceTests $legacy 'legacy-0119' $legacyTests
    $new=Run-SourceTests $current 'current-01110' $currentTests
    $unchanged=(($legacyPlanSHA -eq (Get-FileHash -LiteralPath $legacy.plan -Algorithm SHA256).Hash) -and
       ($currentPlanSHA -eq (Get-FileHash -LiteralPath $current.plan -Algorithm SHA256).Hash) -and
       !((& git -C $LegacyRepo status --porcelain | Out-String).Trim()) -and
       !((& git -C $CurrentRepo status --porcelain | Out-String).Trim()))
    $all=($old.success -and $new.success -and $unchanged)
    $report=[ordered]@{
       schema='air-worker.n044.version-matrix/v1'
       timestamp_utc=[DateTime]::UtcNow.ToString('o')
       legacy=$old;current=$new;source_and_plans_unchanged=$unchanged
       result=$(if($all){'PASS'}else{'FAIL'})
       scope='targeted dual-version signed release tests; not full consumer QA or external judge'
    }
    $json=ConvertTo-Json -InputObject $report -Depth 12
    $reportFile=Join-Path $OutputDir 'N044_VERSION_MATRIX_RESULT.json'
    [IO.File]::WriteAllText($reportFile,$json+[char]10,(New-Object Text.UTF8Encoding($false)))
    Write-Output $json
    if(!$all){exit 1}
    exit 0
} catch {
    Write-Error ('N044 version matrix aborted safely: '+$_.Exception.Message)
    exit 2
} finally {
    $env:TEMP=$origTmp;$env:TMP=$origTemp;$env:GOTMPDIR=$origGoTmp
}