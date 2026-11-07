# AirWorker P0 acceptance readback: no mutation, no new orchestrator.
# Outputs machine JSON with gate count + native node status; RC0 only after P0 DONE,
# RC1 while P0 remains OPEN, RC2 on unreadable/inconsistent evidence.
param(
  [string]$Product = (Join-Path (Split-Path -Parent $PSScriptRoot) ''),
  [string]$Matrix = '',
  [switch]$Json
)
$ErrorActionPreference = 'Stop'
function Fail([string]$Message) {
  $out = [ordered]@{schema='air-worker.p0-metric-readback/v1';status='UNKNOWN';error=$Message}
  Write-Output ($out | ConvertTo-Json -Depth 6 -Compress)
  exit 2
}
try {
  $root = (Resolve-Path -LiteralPath $Product).Path
  if ([string]::IsNullOrWhiteSpace($Matrix)) {
    $Matrix = Join-Path $root 'docs\receipts\DEVWORK29_P0_ACCEPTANCE_MATRIX_20261107.json'
  }
  $matrixDoc = Get-Content -LiteralPath $Matrix -Raw -Encoding UTF8 | ConvertFrom-Json
  if ($matrixDoc.schema -ne 'air-worker.p0-acceptance-matrix/v1') { Fail 'unrecognized acceptance matrix schema' }
  $gates = @($matrixDoc.gates)
  if ($gates.Count -ne 8) { Fail ('acceptance matrix must contain 8 gates: '+$gates.Count) }
  $ids = @($gates | ForEach-Object { $_.id })
  if (@($ids | Sort-Object -Unique).Count -ne 8) { Fail 'duplicated acceptance gate IDs' }
  foreach($g in $gates) {
    if ($g.id -notmatch '^G0[1-8]$' -or $g.status -notin @('PASS','OPEN')) { Fail ('invalid gate status: '+$g.id) }
    if ($g.status -eq 'PASS' -and @($g.evidence).Count -lt 1) { Fail ('PASS without evidence: '+$g.id) }
  }
  $nodeRaw = & air-worker plan node list -product $root -json
  if ($LASTEXITCODE -ne 0) { Fail ('native plan node list failed: '+$LASTEXITCODE) }
  # PowerShell's pipeline ConvertFrom-Json may wrap a JSON array as one
  # object[] item; always iterate decoded node objects, not the outer array.
  $decodedNodes = ConvertFrom-Json -InputObject ($nodeRaw -join "`n")
  $nodes = @()
  foreach ($item in $decodedNodes) { $nodes += $item }
  if ($nodes.Count -lt 4) { Fail 'native node list is incomplete' }
  $nodeReadback = @()
  foreach ($name in @($matrixDoc.native_nodes)) {
    $matches = @($nodes | Where-Object { $_.id -eq $name })
    if ($matches.Count -ne 1) { Fail ('native node ID is absent/duplicate: '+$name) }
    $nodeReadback += [ordered]@{id=$name;status=$matches[0].status}
  }
  $closedNodes = @($nodeReadback | Where-Object {$_.status -eq 'closed'}).Count
  $sourceHead = ((& git -C $root rev-parse HEAD) | Out-String).Trim()
  if ($LASTEXITCODE -ne 0) { Fail 'Git source head unavailable' }
  $worktreeDirty = @(& git -C $root status --porcelain=v1).Count -gt 0
  if ($LASTEXITCODE -ne 0) { Fail 'Git worktree status unavailable' }
  $goalFile = Join-Path $root 'goal\operational-goal.json'
  $goalMatches = $false
  $goalSource = ''
  if (Test-Path -LiteralPath $goalFile -PathType Leaf) {
    $goal = Get-Content -LiteralPath $goalFile -Raw -Encoding UTF8 | ConvertFrom-Json
    $legacyHash = (Get-FileHash -LiteralPath (Join-Path $root 'plan\release-slices.json') -Algorithm SHA256).Hash.ToLowerInvariant()
    $goalSource = $goal.source_head
    $goalMatches = $goal.node_id -eq $matrixDoc.native_nodes[0] -and
      $goal.source_head -eq $sourceHead -and $goal.release_sha256 -eq $legacyHash
  }
  $judgeFile = Join-Path $root '.woody\semantic-verdict.json'
  $judgeState = 'ABSENT'
  if(Test-Path -LiteralPath $judgeFile -PathType Leaf) {
    $judge = Get-Content -LiteralPath $judgeFile -Raw -Encoding UTF8 | ConvertFrom-Json
    $judgeState = [string]$judge.verdict.verdict
  }
  $evidenceWarnings = @()
  if ($gates[0].status -eq 'PASS' -and -not $goalMatches) {
    $evidenceWarnings += 'G01 accepted historical proof but CURRENT operational goal no longer matches HEAD/legacy SHA'
  }
  if ($gates[3].status -eq 'PASS' -and $judgeState -ne 'PASS') {
    Fail 'G04 claimed PASS but independent native Anthropic verdict is not PASS'
  }
  if ($gates[4].status -eq 'PASS' -and $worktreeDirty) {
    Fail 'G05 claimed PASS but source worktree is dirty'
  }
  if ($gates[7].status -eq 'PASS' -and $closedNodes -ne $nodeReadback.Count) {
    Fail 'G08 claimed PASS while native P0 nodes remain open'
  }
  $passed = @($gates | Where-Object { $_.status -eq 'PASS' }).Count
  $remain = $gates.Count - $passed
  $summary = [ordered]@{
    schema='air-worker.p0-metric-readback/v1'
    status=$(if($remain -eq 0 -and $closedNodes -eq 4){'DONE'}else{'OPEN'})
    p0=[ordered]@{
      gates_total=$gates.Count;gates_passed=$passed;gates_remaining=$remain
      accepted_percent=[Math]::Round(100.0*$passed/$gates.Count,1)
      native_nodes_total=$nodeReadback.Count;native_nodes_closed=$closedNodes
      native_nodes_remaining=$nodeReadback.Count-$closedNodes
    }
    source_head=$sourceHead;source_dirty=$worktreeDirty
    operational_goal_matches_current_head=$goalMatches;operational_goal_source_sha=$goalSource
    independent_anthropic_verdict=$judgeState
    p1_example_status=$matrixDoc.p1_case.status
    gate_statuses=@($gates|ForEach-Object{[ordered]@{id=$_.id;status=$_.status;name=$_.name}})
    native_nodes=$nodeReadback
    evidence_warnings=$evidenceWarnings
  }
  Write-Output ($summary | ConvertTo-Json -Depth 12 -Compress)
  if ($summary.status -eq 'DONE') { exit 0 }
  exit 1
} catch {
  Fail $_.Exception.Message
}
