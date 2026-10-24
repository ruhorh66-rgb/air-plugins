# HLP-03 R7 independent source, measurement and full-text review

## Scope

SRVLM01 only; existing checkout; base68f9725c433df319ccb71624f9dce0d5f0d07b9e. Independent native AirWorker executor gpt-6-sol/medium. Read-only except:
docs/receipts/LEARNING_HLP03_R7_INDEPENDENT_REVIEW_20261024.json

No source/plan/config/installed/service/history edits, tests, provider/model jobs, commit/push or AIR-ENV-002. Preserve native PLAN/N/LEARN and WIP. Batch reads; identical seed bodies may be read once per exact SHA while retaining every case association.

## Evidence

- docs/tasks/LEARNING_HLP03_REFINEMENT_R7_20261024.md
- docs/tasks/HLP03_EVALUATION_PROTOCOL_20261024_R7.json
- docs/tasks/HLP03_CORPUS_FREEZE_20261024_R7_DEVELOPMENT.json and R7_HOLDOUT.json; followup freeze/derivation only if created.
- docs/receipts/LEARNING_HLP03_R7_SOURCE_CHANGE_20261024.json
- cmd/learn_adapter.go and its exact diff from the source base.
- .air-worker/learning-loop-20261024/hlp03-r7-source-qa.json
- .air-worker/learning-loop-20261024/hlp03-r7-candidate-snapshot/manifest.json
- .air-worker/learning-loop-20261024/hlp03-r7-preservation-before.json
- .air-worker/learning-loop-20261024/hlp03-r7-postrun-readback.json
- docs/receipts/LEARNING_HLP03_R7_ADVANCE_GATE_20261024.json
- Executed directories .air-worker/learning-loop-20261024/hlp03-r7-{development,holdout,followup}-20261024: original launch.json, terminal.json, resources.jsonl and evidence/summary.json.
- Known corpus/gold: cmd/testdata/hlp03_reviewer_corpus_20261024_r4_development.json; docs/tasks/HLP03_REVIEWER_GOLD_20261024_R4_DEVELOPMENT.json.
- Fresh corpus/gold: cmd/testdata/hlp03_reviewer_corpus_20261024_r5_holdout.json; docs/tasks/HLP03_REVIEWER_GOLD_20261024_R5_HOLDOUT.json.
- Old-case design/gold: docs/tasks/HLP03_FOLLOWUP_DESIGN_20261024_R5.json; docs/tasks/HLP03_REVIEWER_GOLD_20261024_R5_FOLLOWUP.json. If actually executed, use R7 derived corpus and actual seed bindings.
- Prior R6 evaluation, boundary diagnosis and independent review are immutable historical references. PowerShell reuse points to hlp03-r6-source-qa.json and hlp03-r6-source-powershell.txt with exact runner/test/log/receipt hashes.

Absent later runs mean blocked prerequisites when the prospective gate failed. Keep32 designated and actual executed/blocked counts separately; no invented rows or answers.

## Required judgment

1. Verify current adapter/binary/input/protocol hashes against QA, source snapshot and original launches. Only the specified generic prompt replacement may change production code. No author native job was used: the edit and QA are scripts. Host advisory analysis is not native acceptance.
2. Judge source QA within its actual scope. Existing Go targeted tests/vet/build are new R7 evidence. PowerShell tests were NOT rerun in R7: they are reused only through exact verified R6 runner/test/QA/log hashes. Do not claim new failure-E2E or that R6/R7 clean termination fixes the unknown R5 Win32 cause.
3. Read every actual case's full event, applicable complete seed, frozen gold and actual candidate text. Evaluate expected/actual class, exact target/preSHA, retained required old knowledge, full supported new correction and coherent starting state. Accept semantic equivalents; do not use keyword scoring or change labels. In particular assess both sides of the C18 versus C17/C20 boundary without assuming R7 succeeds.
4. Canonical no_candidate is an empty object; errors/refusals are not no_candidate. UPDATE replaces the full body and must preserve all required branches/actions/bounds. CREATE does not itself delete old knowledge. Qualify defects precisely.
5. Verify native completions, cleanup, observed tools/contamination, original wrapper/Go status and resource completeness. Read new diagnostic facts if there is a failure; keep any unknown ultimate cause unknown.
6. Verify prospective advance gates. Coordinator screening can stop further calls; formal content acceptance is this independent review. For old followups, require exact actual R7 applied update bodies copied verbatim and frozen before calls.
7. Keep source acceptance, model semantics, executed measurement, P2/P3 and production effect separate. Finite known synthetic cases do not establish statistical improvement or natural useful effect. Installed0.11.10 remains unchanged. No release version or publication/install acceptance.
8. State the narrowest next step based on actual results; no automatic R8, sweep, fallback or selective retry. Existing scoped-development authorization is not replaced by a generic approval gate.

## Output

Write UTF-8 JSON schema air-worker.hlp03-r7-independent-review/v1 with:
reviewed_at_utc; reviewer(model gpt-6-sol,effort medium,route native executor); source_base_sha; exact_hashes_checked; source_acceptance; measurement_acceptance; model_quality; production_effect NOT_PROVEN; series_results; case_results for actual executed cases; designated_count/executed_count/blocked_count; findings; limitations; next_step.

Each case row: series,id,expected,actual,class_correct,identity_correct,content_acceptable,complete_acceptance,rationale,evidence. Derive totals from those rows, reporting blocked cases outside them. Include concrete evidence references. Do not fabricate completed native job provenance; the coordinator binds the final job receipt/output after completion.

## Observed interruption: required denominator discipline

R7 development wrapper terminated at 2026-10-24T14:22:17Z with WRAPPER_FAILURE during/temp_enumeration, Get-ChildItem line24, Win32Exception NativeErrorCode2. The owned Go tree was terminated; original Go/wrapper exit1, no after resource sample. summary.case_count=24 is the planned count; summary.cases contains only14 completed records C01-C14. stdout started C15, whose seed fixture remains with only two applied seeds and no persisted prompt/config/native job; C16-C24 never started. Keep C15 preparation interruption separate from14 confirmed native reviewer calls; do not invent a fifteenth model completion or score missing cases as no_candidate.
The original partial fixture is retained at .air-worker/learning-loop-20261024/qa-temp/TestHLP03RealReviewerCorpusC1510420697; postrun inventory binds its files. Do not remove it.
Count32 designated =14 completed +1 interrupted preparation +9 unstarted known +6 blocked fresh +2 blocked followups. No rerun and no automatic R8. C18 hypothesis is not measured in this run. Distinguish completed-case content acceptance from rejected/incomplete overall measurement. Read original terminal and source control flow before attributing the ultimate Win32 cause; disappearing temp entry is a hypothesis, not a proved diagnosis.
