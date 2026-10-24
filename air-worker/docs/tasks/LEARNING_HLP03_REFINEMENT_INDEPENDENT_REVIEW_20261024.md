# HLP-03 independent acceptance — R2, R3 and R4

Read-only review on SRVLM01 in the existing product R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker. Ruhor authorized continued source work and self-review. Apply installed Ponytail full mode; use the existing native executor/kernel. This is a separate review job from the source author and the gpt-5.6-luna learning reviewer. Expected dispatch tier is gpt6-sol:medium; provenance must follow the actual executor receipt, not model introspection or a guessed self-name.

Only allowed write:
docs/receipts/LEARNING_HLP03_REFINEMENT_INDEPENDENT_REVIEW_20261024.json
A small review log under the owned .air-worker/learning-loop-20261024 task root is permitted. Do not edit source, tests, corpora, gold, freezes, previous reports, PLAN/N/LEARN, installed runtime/config/services, or any other node. No provider reruns, no release/install, no commit/push. Do not repair or relabel model outputs. Do not rerun tests unless a concrete unresolved source risk requires a bounded offline check.

Review material:
- Current cmd/learn_adapter.go and changed reviewer tests; current cmd/learn_adapter_live_eval_test.go.
- docs/tasks/run-hlp03-real-reviewer-20261024.ps1 and test-run-hlp03-real-reviewer-20261024.ps1.
- Owned hlp03-r2-candidate-snapshot, hlp03-r3-candidate-snapshot, hlp03-r4-candidate-snapshot.
- Owned hlp03-r2-source-qa.json, hlp03-r2-coordinator-qa.txt, hlp03-r3-source-qa.json, hlp03-r4-source-qa.json and their logs.
- Owned hlp03-r4-coordinator-runner-qa.json and coordinator-runner-test.ps1: actual disposable parent+descendant were stopped through the runner helper outside the native author's sandbox. The author observed taskkill/CIM restrictions and recorded them; preserve those limits.
- All original R2/R3/R4 protocols, corpus freezes, gold, follow-up designs and mechanical derivations in docs/tasks.
- Nine run directories under the task root: hlp03-r{2,3,4}-{development,holdout,followup}-20261024, each with evidence/summary.json, per-case seed/packet/prompt/native .out/receipt/result, launch/process/terminal and resources where available.

For efficiency, parse JSON and extract case identity, full canonical candidate content, decision, before/after identities, lifecycle/cleanup/native evidence fields. Read each distinct seed body once. Do not dump repeated full module state maps into your context. Inspect exceptional raw answers and source paths directly as needed; all raw evidence remains available.

Known measurement qualifications to verify, not erase:
- R1 original baseline is immutable and separately reported; do not rescore/rewrite it here.
- R2 has 12 old development +12 fresh-topic +4 old-event followups. Candidate and gold remained fixed across those calls.
- R3 has 12 old development +6 fresh-topic +2 old-event followups. Development wrapper exited when recursive resource enumeration raced with a removed temp entry. Child continued and produced all12 measurement records. Go stdout says PASS/ok because it collects outcomes; C02 still has reviewer_invalid_update. OS exit code was not durably captured; recovery.json and wrapper-failure.txt preserve this, and later holdout/followup terminals do not replace the missing development terminal. Two coordinator metadata probes were stopped; no model rerun occurred.
- R3 C02 raw model chose update but included an extra hex digit in preSHA; the validator rejected it. Count identity failure in the original denominator. It is not a successful update or a provider transport failure.
- R4 keeps R3 semantic prompt, adds eligible-string enums to the existing output schema and retains exact pair/catalogue validation, plus the sampler/fatal-error handling. It has 24 known regression +6 fresh-topic +2 old-event followups. Distinguish these sets; the changed fresh topics do not support a controlled statistical improvement claim.
- PowerShell resource samples can now be incomplete with null bytes; do not turn unknown into zero. Do not claim full DEV-137 compliance from these runs. Current working estimate is 2 GiB, not old 512 MiB/43 MiB and not an OS quota.
- Host advisory agents could not read remote evidence because their calls hung; their comments are not independent acceptance.

Required review:
1. Source acceptance independently: native provider path, schema actually passed from the same trusted fresh catalogue, only eligible identities, empty no-update choices, unmodified final pair/freshness/protected checks; tests and source QA. Separate enum arrays do not enforce pair correctness. Evaluate duplicate identity values if materially relevant. Check source prompt remains generic and does not encode corpus labels/examples.
2. Evaluator/runner evidence: real pinned gpt-5.6-luna medium terminal responses, native receipts/usage, no replay/stub, no observed forbidden source/gold access, cleanup/apply/backup/rollback and hash guards. Detection of observed access is not proof of filesystem isolation. Treat missing terminal/resources as a measurement qualification.
3. Model quality for every designated case against its frozen gold: class AND exact identity, complete semantically correct content, preservation of old applicability/actions/bounds/inputs/order and correct new conditional behavior. Equivalent wording/formulas count; literal keyword matching alone does not. For R2 C08 specifically consider the full old UTF-8 applicability plus added conditional BOM step: assess actual usability and any ambiguity without inventing a posthoc heading-word requirement.
4. Old-event followups must use the actual primary canonical update content copied verbatim. Keep missing prerequisites in the fixed denominator.
5. Production effect remains NOT_PROVEN: fixtures do not execute learned procedures in natural production, installed 0.11.10 delivery was not changed, historical18 migration and native N124 reconciliation are separate.

Output a valid structured JSON receipt with:
schema, recorded_at_utc, reviewer_provenance (declared dispatch vs actual evidence if available), source_acceptance {status, reasons, concrete findings}, measurement {per-series status/limitations}, model_quality {R2,R3,R4 with per-series counts and per_case fields id/expected/actual/class_match/identity_correct/content_verdict/fully_correct/reason plus false_merge/unsafe_scope_loss/incomplete_update when applicable}, followup validation, production_effect, remaining_gates.
Do not merely repeat coordinator conclusions. If source or model fails, identify the exact case or location and concrete reason. A valid refusal/blocked write may prove the source guard works while still failing model quality. Do not invent missing exit codes, usage or provenance. Read back the saved receipt and return a concise verdict with path.
