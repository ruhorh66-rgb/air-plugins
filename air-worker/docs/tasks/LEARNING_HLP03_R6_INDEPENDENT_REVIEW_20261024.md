# HLP-03 R6 independent source, measurement and full-text review

## Scope

Independent review through the existing native AirWorker executor, SRVLM01 only, source base e6c3c7f960208b49bdd0b4e694bc7eb99554bdce. You are independent of the native R6 author and coordinator's screening. Source acceptance, model semantics, measurement reliability, P2/P3 and production effect are separate.

Read-only except one output:
docs/receipts/LEARNING_HLP03_R6_INDEPENDENT_REVIEW_20261024.json

Do not edit code, plans, frozen inputs, gold, snapshots, original run evidence, native PLAN/N/LEARN, configs, installed files, services or WIP. No AIR-ENV-002. No tests, providers, new model jobs, install, commit or push. Do not inspect credentials.

## Primary evidence

- docs/tasks/HLP03_EVALUATION_PROTOCOL_20261024_R6.json
- docs/tasks/HLP03_CORPUS_FREEZE_20261024_R6_DEVELOPMENT.json
- docs/tasks/HLP03_CORPUS_FREEZE_20261024_R6_HOLDOUT.json
- docs/tasks/LEARNING_HLP03_REFINEMENT_R6_20261024.md
- docs/receipts/LEARNING_HLP03_R6_AUTHOR_20261024.json
- cmd/learn_adapter.go
- docs/tasks/run-hlp03-real-reviewer-20261024.ps1
- docs/tasks/test-run-hlp03-real-reviewer-20261024.ps1
- .air-worker/learning-loop-20261024/hlp03-r6-source-qa.json and hlp03-r6-source-powershell.txt
- .air-worker/learning-loop-20261024/hlp03-r6-candidate-snapshot/manifest.json
- .air-worker/learning-loop-20261024/hlp03-r6-preservation-before.json
- .air-worker/learning-loop-20261024/hlp03-r6-postrun-readback.json
- docs/receipts/LEARNING_HLP03_R6_ADVANCE_GATE_20261024.json
- Each executed run directory under .air-worker/learning-loop-20261024/:
  hlp03-r6-development-20261024, hlp03-r6-holdout-20261024, hlp03-r6-followup-20261024
  Read launch.json, terminal.json, resources.jsonl and evidence/summary.json when present.
- Original known corpus/gold: cmd/testdata/hlp03_reviewer_corpus_20261024_r4_development.json and docs/tasks/HLP03_REVIEWER_GOLD_20261024_R4_DEVELOPMENT.json.
- Fresh corpus/gold reused unchanged from R5: cmd/testdata/hlp03_reviewer_corpus_20261024_r5_holdout.json and docs/tasks/HLP03_REVIEWER_GOLD_20261024_R5_HOLDOUT.json.
- Followup design/gold reused unchanged from R5: docs/tasks/HLP03_FOLLOWUP_DESIGN_20261024_R5.json and docs/tasks/HLP03_REVIEWER_GOLD_20261024_R5_FOLLOWUP.json. Only if actually run, read the derived R6 corpus/freeze/derivation files referenced by its launch.
- docs/receipts/LEARNING_HLP03_REFINEMENT_R5_EVAL_20261024.json for immutable history and precise qualifications, not as measured R6 outcomes.

An absent later run is expected if the advance gate failed. Preserve32 designated, actual executed count and blocked count separately. Do not infer uncollected answers.

## Required judgment

1. Verify exact current code/input/binary hashes against QA, snapshots and launches. Review the complete diff and diagnostic logic. The R6 author changed prompt and added bounded original-error diagnostics; Stop-AwOwnedGo and core/module/schema/Go-harness behavior are intended unchanged. The coordinator then added a deterministic disk-query fault through actual Save-AwSample to verify real phase/operation capture, so the final focused-test SHA differs from the author receipt's test SHA. The author receipt must remain original; judge the final QA-bound source.
2. Evaluate the focused PowerShell5.1 diagnostic evidence honestly. Tests include real caught Win32Exception/error records, bounds, JSON round trips, unavailable native code, a fault through Save-AwSample and existing footprint/owned-child checks. Do not claim whole live-process E2E from an in-process helper test; inspect actual new live terminal evidence separately.
3. Read every executed case's complete event, seed, frozen gold and actual candidate body. Report expected/actual class, exact target/preSHA identity, content acceptance and complete acceptance with rationale. A canonical no_candidate is empty object. Updates replace the entire body and must retain required old applicability/actions/bounds. Creates must teach supported distinct correction and consistent starting state; do not infer old knowledge was overwritten by a CREATE.
4. Accept semantic equivalents, not keyword matching. Do not relabel gold. Compare known regression and fresh topics separately; a finite first-attempt corpus does not establish statistical improvement or natural usefulness.
5. Inspect native completions/cleanup/tool calls/contamination and all original wrapper outcomes. If failure occurs, retain it and use the new actual diagnostics to distinguish established operation/location from still-unknown ultimate cause. No reconstruction of successful terminal or resource after sample.
6. Verify the advance gate follows the prospective protocol, including coordinator screening only as a conservative stop, not formal acceptance. If followups ran, check exact actual R6 updated bodies were copied and frozen before old events. If no designated update exists, keep the prerequisite failure visible.
7. Source tests, syntax, full model outcomes and installed effect have independent verdicts. P2/N124 and P3 remain unaccepted in this source task. Installed0.11.10 is not modified. No invented release version, deployment or production-use claim.
8. Identify the narrowest evidence-based next step. No automatic R7, model sweep, fallback or selective retry. Existing user authorization for scoped source work remains; your advisory next step does not create a new generic approval gate.

## Output

Write UTF-8 JSON schema air-worker.hlp03-r6-independent-review/v1:
reviewed_at_utc; reviewer(model gpt-6-sol,effort medium,route native executor); source_base_sha; exact_hashes_checked; source_acceptance; measurement_acceptance; model_quality; production_effect NOT_PROVEN; series_results and case_results for actual executed cases only; designated/executed/blocked counts; findings; limitations; next_step.

Each case: series,id,expected,actual,class_correct,identity_correct,content_acceptable,complete_acceptance,rationale. Compute totals from these rows; blocked cases remain explicit outside actual case rows. Include concrete evidence references and accurately qualified defects.

Do not fabricate completed native job provenance. The coordinator binds the final native job receipt/output hash after completion.
