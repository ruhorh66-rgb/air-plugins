# HLP-03 R5 independent source and full-content review (2026-10-24)

## Purpose and authority

Use the existing native AirWorker executor on SRVLM01 only, in this product checkout. This is an independent review of the current uncommitted R5 source candidate and immutable measured outputs, not a production release or installed-runtime acceptance. The coordinator authored the R5 paragraph; you did not. Keep source acceptance, model semantic quality and production effect separate.

Read-only investigation. Your only permitted writes are:
- docs/receipts/LEARNING_HLP03_R5_INDEPENDENT_REVIEW_20261024.json

Do not edit code, plans, corpora, gold, snapshots, original run artifacts, WIP, installed files, services or native PLAN/N/LEARN. Do not use AIR-ENV-002. Do not invoke another provider/reviewer, rerun Go/model tests, dispatch jobs, install, commit or push. Do not inspect credentials or external services.

## Exact subject

Source base HEAD: 92fd3ce9ad71f6ea5fe7c8252a341191ce4d6619.
cmd/learn_adapter.go expected SHA256 c06762214fb4ff0cd64cda52e5aa18f11cfec70cdab49ea3edf15f9c7876f9f7.
cmd/learn_adapter_live_eval_test.go SHA256 961277bac635a49226bf36d1444c20fe6290d25fc831ce3e397fa5cceab1a49b.
docs/tasks/run-hlp03-real-reviewer-20261024.ps1 unchanged SHA256 e91c63125bf208ca84acfd9791001d8a40cf431a05004be6b269b8149990dfde.
Measured task binary SHA256 934eb3b5adc70cd886d7f204dacb74d90eb316a0b25516cb9cb507fd07371302.

The production-code diff is one generic paragraph added to reviewer prompt. Inspect the actual git diff and complete surrounding semantics. No claims of natural production benefit or semantic correctness follow from test PASS.

## Read these primary materials

- docs/tasks/HLP03_EVALUATION_PROTOCOL_20261024_R5.json
- docs/tasks/HLP03_CORPUS_FREEZE_20261024_R5_DEVELOPMENT.json
- cmd/testdata/hlp03_reviewer_corpus_20261024_r4_development.json
- docs/tasks/HLP03_REVIEWER_GOLD_20261024_R4_DEVELOPMENT.json
- .air-worker/learning-loop-20261024/hlp03-r5-source-qa.json
- .air-worker/learning-loop-20261024/hlp03-r5-source-go-test.jsonl (targeted summary/skip reasons if needed)
- .air-worker/learning-loop-20261024/hlp03-r5-candidate-snapshot/manifest.json
- .air-worker/learning-loop-20261024/hlp03-r5-development-20261024/launch.json
- .air-worker/learning-loop-20261024/hlp03-r5-development-20261024/terminal.json
- .air-worker/learning-loop-20261024/hlp03-r5-development-20261024/resources.jsonl
- .air-worker/learning-loop-20261024/hlp03-r5-development-20261024/evidence/summary.json
- .air-worker/learning-loop-20261024/hlp03-r5-postfailure-readback.json
- docs/receipts/LEARNING_HLP03_R5_ADVANCE_GATE_20261024.json
- docs/receipts/AIRWORKER_INTERMEDIATE_RELEASE_PREPARATION_20261024.json
- docs/receipts/LEARNING_HLP03_REFINEMENT_EVAL_20261024.json (prior R4 qualifications only; immutable history)

Use actual paths listed above; if a file is absent, report it rather than inventing evidence. Bound all file searches to these directories. UTF-8 reads are required for PowerShell 5.1.

## Decisions required

1. Verify exact source/input/binary hash bindings. Give independent source acceptance with explicit limits. Existing targeted source QA records 74 passing test records and two skips (real reviewer env-unset; Windows unsafe symlink privilege unavailable), successful vet/build/diff. These are not a full suite or new package acceptance.
2. For every one of the 24 collected cases, read the complete event, seed, frozen gold and actual candidate body. Record expected/actual class, exact target/preSHA identity, semantic content acceptance and rationale. A canonical no_candidate is an empty object, not null. An update must preserve applicable old procedure knowledge while adding supported new behavior; a create must teach the distinct supported corrective action without contradictory steps. Accept semantic equivalents; no literal keyword grading.
3. Compute class+identity and complete acceptance counts. Expected classes are balanced 8/8/8. Do not change gold or drop wrong/rejected cases. The coordinator's advance check found C02 and C17 create instead of update; independently verify. Qualify all content findings precisely from actual wording. Previous R4 findings were qualified: C11 did retain same input, three attempts, deadline and no extension, but did not expressly check whether the added wait fits; C15 was a CREATE with a no-repeat pitfall but an applicability/action inconsistency. Do not reuse incorrect old labels as evidence about R5.
4. Keep 32 designated / 24 executed / 8 blocked visible. R5 fresh6 and followup2 were NOT_RUN_BLOCKED by the prospective technical/class/identity gate and cannot pass. Do not read them as measured outcomes or infer any uncollected answer. R5 has no selective retries or next prompt candidate.
5. Analyze measurement reliability without changing original evidence. The wrapper exited1 with WRAPPER_FAILURE/System.ComponentModel.Win32Exception, Go exit0 was durably recorded, all24 native turns completed and all24 cleanups were reported. A transient Get-ChildItem disappearance was observed; the original terminal lacks stack/position. Recorded resource samples end at phase during, no after sample. Do not infer a definite fatal root cause from a nearby enumeration warning. Explain whether existing evidence establishes the cause; if not, report UNKNOWN and the minimum useful deterministic next diagnostic. Original failure must not be rewritten into success. The separate postfailure readback is separate evidence of final hashes/absence, not a reconstructed original terminal.
6. Check the prepared intermediate release receipt for overclaim: QA binary lacks embedded VCS revision/modified metadata, no release version assigned, P2/N124 authority unresolved, package smoke/rollback not run. No release/package/install/production acceptance follows.
7. Recommend the narrowest next useful step from the evidence, with no automatic R6 or speculative framework. Separate advisory recommendation from pass/fail.

## Output schema (JSON)

Write well-formed UTF-8 JSON with schema air-worker.hlp03-r5-independent-review/v1, reviewed_at_utc, reviewer (model gpt-6-sol, effort medium, route native executor, do not fabricate final native job receipt), reviewed_source_base_sha, exact source/input hashes actually checked, source_acceptance, measurement_acceptance, model_quality, production_effect NOT_PROVEN, case_results array24, counts, findings, limitations, next_step.

Each case: id, expected, actual, class_correct, identity_correct, content_acceptable (true/false/null with why), complete_acceptance, rationale. Retain substantive textual findings with actual file/case references.

Report native executor provenance only as observed. The coordinator will bind your completed native job receipt and output hash after the job finishes. Do not invent a completed receipt or make approval claims.
