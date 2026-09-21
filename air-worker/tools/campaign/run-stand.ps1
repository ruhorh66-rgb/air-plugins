# Plan step 9: stand on the candidate binary. Run with pwsh 7 (file is UTF-8 without BOM, plan text is Cyrillic).
#   stand - receipt race: 30 launches of the semantic reviewer (fake codex) while adapter status is polled back to back;
#           every receipt must carry the reviewer's real PID and process_started=true (0 lost, 0 aborted).
#   m6    - mutation "reviewer launched without a receipt": the test TestSemanticReviewerRegistersReceipt must fail on the
#           mutant and pass on the original.
#   c7    - Ц7-1..3 and Ц7-4 on a mini product: open gate, malformed gate, orchestrate at an open gate, PSModulePath.
# Writes .campaign/results/{stand,m6,c7,d5m,d6m}.json. Exit 1 if any of them is red.
param([string]$BinPath = '')
$ErrorActionPreference = 'Continue'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
. (Join-Path $PSScriptRoot 'lib.ps1')
$root = $script:Root
$exe = $script:CandExe
if ($BinPath) { $exe = $BinPath }
if (-not (Test-Path -LiteralPath $exe)) { Write-Output 'candidate binary is missing: run tools/campaign/run-suite.ps1 first'; exit 1 }
$srcHash = Get-SrcHash
$work = Join-Path $root '.campaign\stand'
Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $work | Out-Null
$gocache = (& go env GOCACHE).Trim(); $gopath = (& go env GOPATH).Trim()

function Write-Utf8([string]$Path, [string]$Text) { [System.IO.File]::WriteAllText($Path, $Text, (New-Object System.Text.UTF8Encoding($false))) }

# Isolated state for this process only; go keeps the real build cache.
$envr = Join-Path $work 'env'
$env:LOCALAPPDATA = "$envr\localappdata"; $env:APPDATA = "$envr\appdata"; $env:ProgramData = "$envr\programdata"
$env:TEMP = "$envr\temp"; $env:TMP = "$envr\temp"; $env:AIR_WORKER_HOME = "$envr\localappdata\air-worker"; $env:AIR_WORKER_RUNTIME = "$envr\runtime"
$env:GOCACHE = $gocache; $env:GOPATH = $gopath; $env:GOTELEMETRY = 'off'
foreach ($d in @($env:LOCALAPPDATA, $env:APPDATA, $env:ProgramData, $env:TEMP, $env:AIR_WORKER_RUNTIME)) { New-Item -ItemType Directory -Force -Path $d | Out-Null }

# ---------------- stand: receipt race ----------------
$h = Join-Path $work 'helpers'
New-Item -ItemType Directory -Force -Path $h | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'tools\campaign\stand\fakecodex2.go.txt') -Destination (Join-Path $h 'fakecodex2.go')
Push-Location $h
$env:CGO_ENABLED = '0'
& go build -o fakecodex2.exe fakecodex2.go
Pop-Location
$env:AIR_CODEX_EXE = Join-Path $h 'fakecodex2.exe'; $env:FAKE_CODEX_SLEEP = '1'

function New-RevProduct([string]$Dir) {
    New-Item -ItemType Directory -Force -Path $Dir | Out-Null
    Write-Utf8 (Join-Path $Dir 'run-config.json') '{"judge":{"path":"judge.ps1","checklist":"checklist.json"},"ladder":["script"]}'
    Write-Utf8 (Join-Path $Dir 'checklist.json') '{"items":[{"id":"f01","status":"pending","awaits":""}]}'
    Write-Utf8 (Join-Path $Dir 'judge.ps1') "exit 1`n"
    $plan = @'
**Ц1.** ревью видно

| Критерий | Цель | Признак достижения | Чем меряется |
|---|---|---|---|
| К1 | Ц1 | шаги закрыты | факт `f01` |

| № | Шаг | Ступень | Судья |
|---|---|---|---|
| ~~1~~ | реализация :: exit 0 | script | К1 |
| 2 | следующий шаг :: exit 0 | script | К1 |
'@
    Write-Utf8 (Join-Path $Dir 'PLAN.md') $plan
}

$N = 30; $okN = 0; $lost = 0; $aborted = 0
for ($i = 1; $i -le $N; $i++) {
    $d = Join-Path $work "prod_$i"
    New-RevProduct $d
    $pf = Join-Path $d 'fake.pid'
    $env:FAKE_CODEX_PIDFILE = $pf
    $poll = Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoProfile', '-File', (Join-Path $root 'tools\campaign\stand\statuspoll.ps1'), '-Exe', $exe, '-Dir', $d) -PassThru -WindowStyle Hidden
    $p = Start-Process -FilePath $exe -ArgumentList @('semantic', '-product', $d, '-step', '1', '-executor', 'claude') -PassThru -WindowStyle Hidden -RedirectStandardOutput (Join-Path $d 'sem.out.txt') -RedirectStandardError (Join-Path $d 'sem.err.txt')
    $p.WaitForExit(120000) | Out-Null
    Set-Content -LiteralPath (Join-Path $d 'stop.flag') -Value 'stop'
    $poll.WaitForExit(20000) | Out-Null
    if (-not $poll.HasExited) { Stop-Process -Id $poll.Id -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Milliseconds 300
    $fakePid = -1
    if (Test-Path -LiteralPath $pf) { $fakePid = [int](Get-Content -Raw -LiteralPath $pf).Trim() }
    $rf = Get-ChildItem -LiteralPath (Join-Path $d '.woody\jobs') -Filter *.receipt.json -ErrorAction SilentlyContinue | Select-Object -First 1
    if ((-not $rf) -or $fakePid -lt 0) { $aborted++; continue }
    $r = Get-Content -Raw -LiteralPath $rf.FullName | ConvertFrom-Json
    if ($r.pid -eq $fakePid -and $r.process_started) { $okN++ } else { $lost++ }
}
# hold: a reader keeps the fresh receipt open for 700 ms at launch (shorter than the 2 s retry window):
# the PID must still be registered. On a binary without the retry the reviewer stays invisible.
Copy-Item -LiteralPath (Join-Path $root 'tools\campaign\stand\rcptpoll.go.txt') -Destination (Join-Path $h 'rcptpoll.go')
Push-Location $h
& go build -o rcptpoll.exe rcptpoll.go
Pop-Location
if (-not (Test-Path -LiteralPath (Join-Path $h 'rcptpoll.exe'))) { Write-Output 'holder helper was not built'; Write-Result 'stand' $false 'holder helper was not built'; exit 1 }
$HN = 10; $holdOk = 0; $holdBad = 0
for ($i = 1; $i -le $HN; $i++) {
    $d = Join-Path $work "hold_$i"
    New-RevProduct $d
    $pf = Join-Path $d 'fake.pid'
    $env:FAKE_CODEX_PIDFILE = $pf
    $holder = Start-Process -FilePath (Join-Path $h 'rcptpoll.exe') -ArgumentList @('hold', (Join-Path $d '.woody\jobs'), '40', '700') -PassThru -WindowStyle Hidden -RedirectStandardOutput (Join-Path $d 'holder.out.txt')
    $p = Start-Process -FilePath $exe -ArgumentList @('semantic', '-product', $d, '-step', '1', '-executor', 'claude') -PassThru -WindowStyle Hidden -RedirectStandardOutput (Join-Path $d 'sem.out.txt') -RedirectStandardError (Join-Path $d 'sem.err.txt')
    $p.WaitForExit(120000) | Out-Null
    $holder.WaitForExit(20000) | Out-Null
    if (-not $holder.HasExited) { Stop-Process -Id $holder.Id -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Milliseconds 300
    $fakePid = -1
    if (Test-Path -LiteralPath $pf) { $fakePid = [int](Get-Content -Raw -LiteralPath $pf).Trim() }
    $rf = Get-ChildItem -LiteralPath (Join-Path $d '.woody\jobs') -Filter *.receipt.json -ErrorAction SilentlyContinue | Select-Object -First 1
    $good = $false
    $opened = (Test-Path -LiteralPath (Join-Path $d 'holder.out.txt')) -and ((Get-Content -Raw -LiteralPath (Join-Path $d 'holder.out.txt')) -match 'hold: opened')
    if ($opened -and $rf -and $fakePid -gt 0) {
        $r = Get-Content -Raw -LiteralPath $rf.FullName | ConvertFrom-Json
        $good = ($r.pid -eq $fakePid -and $r.process_started -and $r.status -eq 'DONE')
    }
    if ($good) { $holdOk++ } else { $holdBad++ }
}
$env:FAKE_CODEX_PIDFILE = ''
$standOk = ($lost -eq 0 -and $aborted -eq 0 -and $okN -eq $N -and $holdBad -eq 0)
$standDetail = "polled runs $N, ok $okN, lost $lost, aborted $aborted; held-receipt runs $HN, ok $holdOk, bad $holdBad"
Write-Output ('stand: ' + $standDetail)
Write-Result 'stand' $standOk $standDetail

# ---------------- m6: mutation "reviewer without a receipt" ----------------
$m6 = Join-Path $work 'm6'
New-Item -ItemType Directory -Force -Path $m6 | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'cmd') -Destination (Join-Path $m6 'cmd') -Recurse -Force
$env:AIR_CODEX_EXE = ''; $env:FAKE_CODEX_PIDFILE = ''
$sel = '^TestSemanticReviewerRegistersReceipt$'
$base = (& go -C (Join-Path $m6 'cmd') test ./... -count=1 -run $sel 2>&1 | Out-String)
$baseOk = ($LASTEXITCODE -eq 0)
$semFile = Join-Path $m6 'cmd\semantic.go'
$t = [System.IO.File]::ReadAllText($semFile, [System.Text.Encoding]::UTF8)
$rx = [regex]'out, runErr := runReceiptedWithMeta\(context\.Background\(\), scope, step, "semantic-reviewer", cmd, jobReceiptMeta\{[^}]*\}\)'
$mutCount = $rx.Matches($t).Count
$t = $rx.Replace($t, 'out, runErr := cmd.CombinedOutput()') + "`nvar _ = context.Background`n"
[System.IO.File]::WriteAllText($semFile, $t, (New-Object System.Text.UTF8Encoding($false)))
$mut = (& go -C (Join-Path $m6 'cmd') test ./... -count=1 -run $sel 2>&1 | Out-String)
$mutFailed = ($LASTEXITCODE -ne 0)
$killed = $mutFailed -and ($mut -match '--- FAIL: TestSemanticReviewerRegistersReceipt') -and ($mut -notmatch '\[build failed\]')
$m6Ok = ($baseOk -and ($mutCount -ge 1) -and $killed)
$m6Detail = "test passes on original: $baseOk; mutations applied: $mutCount; test fails on mutant: $killed"
Write-Output ('m6: ' + $m6Detail)
Write-Result 'm6' $m6Ok $m6Detail

# ---------------- c7: gate barrier, malformed gate, orchestrate, PSModulePath ----------------
function New-GateProduct([string]$Dir, [string]$GateRows) {
    New-Item -ItemType Directory -Force -Path $Dir | Out-Null
    Write-Utf8 (Join-Path $Dir 'run-config.json') '{"judge":{"path":"judge.ps1"},"ladder":["script"],"budget":{"iterations":6}}'
    Write-Utf8 (Join-Path $Dir 'judge.ps1') "exit 1`n"
    $plan = "**Ц1.** проверка гейтов`n`n| Критерий | Цель | Признак достижения | Чем меряется |`n|---|---|---|---|`n| К1 | Ц1 | маркеры записаны | факт ``f01`` |`n`n| № | Шаг | Ступень | Судья |`n|---|---|---|---|`n| ~~1~~ | шаг один :: exit 0 | script | К1 |`n" + $GateRows + "| 4 | шаг ниже :: Set-Content -LiteralPath 'BELOW.txt' -Value b | script | К1 |`n"
    Write-Utf8 (Join-Path $Dir 'PLAN.md') $plan
}
$c7Fail = New-Object System.Collections.Generic.List[string]
$scen = @(
    @{ n = 'open gate, loop'; cmd = 'loop'; rows = "| 2 | решение ЛПР | — | гейт: ЛПР |`n" },
    @{ n = 'malformed gate row, loop'; cmd = 'loop'; rows = "| 2 | решение ЛПР | LPR??? | ждём разрешения ЛПР |`n" },
    @{ n = 'open gate, orchestrate'; cmd = 'orchestrate'; rows = "| 2 | выпуск | — | гейт: ЛПР |`n" }
)
$k = 0
foreach ($s in $scen) {
    $k++
    $d = Join-Path $work "gate_$k"
    New-GateProduct $d $s.rows
    & $exe $s.cmd -product $d *> $null
    $code = $LASTEXITCODE
    $below = Test-Path -LiteralPath (Join-Path $d 'BELOW.txt')
    if ($code -ne 3 -or $below) { $c7Fail.Add($s.n + ' (exit ' + $code + ', step below executed: ' + $below + ')') }
}
# PSModulePath with the PowerShell 7 directory first: Get-FileHash must work in the judge check (Windows PowerShell 5.1 child).
$dp = Join-Path $work 'psmod'
New-Item -ItemType Directory -Force -Path $dp | Out-Null
Write-Utf8 (Join-Path $dp 'run-config.json') '{"judge":{"checks":[{"name":"hash","script":"check.ps1","args":[]}]},"ladder":["script"]}'
Set-Content -LiteralPath (Join-Path $dp 'check.ps1') -Encoding ASCII -Value 'try { $h = (Get-FileHash -LiteralPath $PSCommandPath -Algorithm SHA256).Hash; if ($h.Length -eq 64) { exit 0 } else { exit 1 } } catch { exit 1 }'
Write-Utf8 (Join-Path $dp 'PLAN.md') "**Ц1.** проверка среды дочернего PowerShell`n`n| Критерий | Цель | Признак достижения | Чем меряется |`n|---|---|---|---|`n| К1 | Ц1 | Get-FileHash работает | проверка ``hash`` |`n`n| № | Шаг | Ступень | Судья |`n|---|---|---|---|`n| ~~1~~ | сделано :: exit 0 | script | К1 |`n"
$savedPsm = $env:PSModulePath
$env:PSModulePath = 'C:\Program Files\PowerShell\7\Modules;C:\Windows\System32\WindowsPowerShell\v1.0\Modules'
& $exe judge -product $dp *> $null
$psmCode = $LASTEXITCODE
$env:PSModulePath = $savedPsm
if ($psmCode -ne 0) { $c7Fail.Add('PSModulePath scenario: judge exit ' + $psmCode) }
$c7Ok = ($c7Fail.Count -eq 0)
$c7Detail = if ($c7Ok) { 'gate barrier x3 and PSModulePath scenario hold' } else { $c7Fail -join '; ' }
Write-Output ('c7: ' + $c7Detail)
Write-Result 'c7' $c7Ok $c7Detail

# ---------------- d5m: mutations of D-5 (the core owns closing a model step, PLAN.md is outside the executor's writes) ----------------
$d5Fail = New-Object System.Collections.Generic.List[string]
$d5Baseline = (& go -C (Join-Path $root 'cmd') test ./... -count=1 -run '^(TestPlanRowReturnedByteForByteOnNotPass|TestExecutorCannotModifyPlan|TestEngineClosesModelStepOnPass)$' 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) { $d5Fail.Add('D-5 tests are not green on the original') }
$d5Muts = @(
    @{ label = 'restore'; pattern = 'func restorePlanBytes\([^)]*\) error \{'; expect = @('TestPlanRowReturnedByteForByteOnNotPass', 'TestExecutorCannotModifyPlan') },
    @{ label = 'close'; pattern = 'func closeModelStepOnPass\([^)]*\) error \{'; expect = @('TestEngineClosesModelStepOnPass') }
)
foreach ($mut in $d5Muts) {
    $mdir = Join-Path $work ('d5_' + $mut.label)
    New-Item -ItemType Directory -Force -Path $mdir | Out-Null
    Copy-Item -LiteralPath (Join-Path $root 'cmd') -Destination (Join-Path $mdir 'cmd') -Recurse -Force
    $applied = $false
    foreach ($gf in (Get-ChildItem -LiteralPath (Join-Path $mdir 'cmd') -Filter *.go -File)) {
        if ($gf.Name -like '*_test.go') { continue }
        $body = [System.IO.File]::ReadAllText($gf.FullName, [System.Text.Encoding]::UTF8)
        $mm = [regex]::Match($body, $mut.pattern)
        if ($mm.Success) {
            $body = $body.Insert($mm.Index + $mm.Length, "`n`treturn nil")
            [System.IO.File]::WriteAllText($gf.FullName, $body, (New-Object System.Text.UTF8Encoding($false)))
            $applied = $true
            break
        }
    }
    if (-not $applied) { $d5Fail.Add('mutation ' + $mut.label + ': function not found'); continue }
    foreach ($tn in $mut.expect) {
        $mout = (& go -C (Join-Path $mdir 'cmd') test ./... -count=1 -run ('^' + $tn + '$') 2>&1 | Out-String)
        $dead = ($LASTEXITCODE -ne 0) -and ($mout -match ('--- FAIL: ' + $tn)) -and ($mout -notmatch '\[build failed\]')
        if (-not $dead) { $d5Fail.Add('mutation ' + $mut.label + ' not killed by ' + $tn) }
    }
}
$d5Ok = ($d5Fail.Count -eq 0)
$d5Detail = if ($d5Ok) { 'both D-5 mutations killed by their tests' } else { $d5Fail -join '; ' }
Write-Output ('d5m: ' + $d5Detail)
Write-Result 'd5m' $d5Ok $d5Detail

# ---------------- d6m: mutation of D-6 (judge files are outside the executor's writes) ----------------
$d6Fail = New-Object System.Collections.Generic.List[string]
$d6Test = 'TestJudgeFilesRestoredAfterExecutorTurn'
$null = (& go -C (Join-Path $root 'cmd') test ./... -count=1 -run ('^' + $d6Test + '$') 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) { $d6Fail.Add('D-6 test is not green on the original') }
$d6dir = Join-Path $work 'd6_restore'
New-Item -ItemType Directory -Force -Path $d6dir | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'cmd') -Destination (Join-Path $d6dir 'cmd') -Recurse -Force
$d6applied = $false
foreach ($gf in (Get-ChildItem -LiteralPath (Join-Path $d6dir 'cmd') -Filter *.go -File)) {
    if ($gf.Name -like '*_test.go') { continue }
    $body = [System.IO.File]::ReadAllText($gf.FullName, [System.Text.Encoding]::UTF8)
    $mm = [regex]::Match($body, 'func restoreJudgeFiles\([^)]*\) \([^)]*\) \{')
    if ($mm.Success) {
        $body = $body.Insert($mm.Index + $mm.Length, "`n`treturn nil, nil")
        [System.IO.File]::WriteAllText($gf.FullName, $body, (New-Object System.Text.UTF8Encoding($false)))
        $d6applied = $true
        break
    }
}
if (-not $d6applied) { $d6Fail.Add('mutation: restoreJudgeFiles not found') }
else {
    $d6out = (& go -C (Join-Path $d6dir 'cmd') test ./... -count=1 -run ('^' + $d6Test + '$') 2>&1 | Out-String)
    $d6dead = ($LASTEXITCODE -ne 0) -and ($d6out -match ('--- FAIL: ' + $d6Test)) -and ($d6out -notmatch '\[build failed\]')
    if (-not $d6dead) { $d6Fail.Add('mutation not killed by ' + $d6Test) }
}
$d6Ok = ($d6Fail.Count -eq 0)
$d6Detail = if ($d6Ok) { 'D-6 mutation killed by its test' } else { $d6Fail -join '; ' }
Write-Output ('d6m: ' + $d6Detail)
Write-Result 'd6m' $d6Ok $d6Detail

if ($srcHash -ne (Get-SrcHash)) { Write-Output 'sources changed during the stand run: results are stale'; exit 1 }
if ($standOk -and $m6Ok -and $c7Ok -and $d5Ok -and $d6Ok) { Write-Output 'stand OK'; exit 0 }
exit 1
