# HLP-03 R2 — preserve prior applicability and mandatory actions

LPR 2026-10-24: recheck your work, follow the plan, continue. SRVLM01 only. Existing source checkout R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker; base HEAD 2524960bdb950bc075bb7cbebccbcf9bf933efb8. Use installed Ponytail full mode and existing native/kernel mechanisms. Do not create another checkout, service, semantic dedup framework or new dependency.

Read docs/receipts/LEARNING_HLP03_REAL_REVIEWER_EVAL_20261024.json and the independent review, plus cmd/learn_adapter.go, cmd/learn_adapter_reuse_test.go, cmd/learn_adapter_live_eval_test.go and docs/tasks/run-hlp03-real-reviewer-20261024.ps1. The prior class/identity score 11/12 is not content acceptance: two updates lost old applicability; one lost explicit required details. Preserve the first baseline and all frozen inputs/outputs byte for byte. Do not inspect any new R2 holdout/gold/corpus files: they are prepared separately for the next measurement.

Allowed source mutation:
- cmd/learn_adapter.go: minimal generic clarification of buildLearningReviewerPromptWithSettings, preserving schema, validators and native provider path.
- cmd/learn_adapter_live_eval_test.go: remove the special hardcoded 12-case restriction (existing 1..24 bound is sufficient); keep default opt-in SKIP, model pin, evidence checks and corrected cleanup registration. Add actual case_count to identity/summary if helpful. Generalize forbidden gold/freeze basename markers only as needed to support a later corpus. No provider stub/replay or alternate prompt in live evaluator.
- docs/tasks/run-hlp03-real-reviewer-20261024.ps1: minimally parameterize existing runner for CorpusFreeze path, output directory basename, run ID, source binary path and SHA, with current baseline defaults retained. The default still refuses an existing output. Preserve byte-frozen baseline artifacts. Check actual corpus case count equals frozen case_count. Capture actual script/file hashes and run identity, bounded deadline, root metadata snapshots. Do not copy the entire launcher for each new run or build a new framework.
- Existing reviewer tests only if a concrete contract needs verification; do not add tautological English substring tests to claim model semantics.

Prompt contract:
- Existing appropriate procedure first, no_candidate for already-covered lesson.
- Useful additional input variant, conditional branch or missing step in the same workflow can refine the existing procedure; a different detail alone should not force a separate skill.
- An update is a complete replacement, not a delta. Start from the full supplied current body and make the smallest needed edit. Retain all previously valid applicability branches, mandatory actions, explicit bounds, required inputs, ordering and relevant pitfalls. Extend applicability with the new branch; never replace it with only the latest event's narrow condition.
- Add the learned detail without turning optional/conditional new behavior into a new prerequisite for all old uses. If evidence does not justify safely changing old requirements, do not silently weaken them.
- Truly separate causes/actions remain separate skills. Do not introduce corpus names, IDs, topics, example answers or literal baseline expectations into the production prompt.
- Keep untrusted-data boundary, exact eligible target/preSHA, full fresh catalogue, machine/protected exclusions and existing module-owned apply/backup/rollback.

Runner parameters must remain scoped to existing product and owned task root. Output must not escape task root; source binary path must be absolute and SHA verified, immutable original baseline refused. Keep installed binary/config/native PLAN/N/LEARN/go.mod and all foreign WIP unchanged. No commit/push/release/install/UAC/reboots/services/other node.

Validation in ONE own QA at a time using inherited GOCACHE/GOTMPDIR/TEMP/TMP. Current DEV-137 v1.1.0 working estimate 2 GiB, not old 512 MiB hard guard; sample owned cache/temp before/after, do not clean shared paths.
1. gofmt any changed Go source.
2. With AIR_WORKER_HLP03_LIVE unset: go test . -run '^(TestLearningReviewer|TestHLP03)' -count=1 -timeout 180s.
3. go vet .
4. go build -o <taskroot>\air-worker-hlp03-r2.exe .
5. Parse the PowerShell runner syntax only, no live evaluation.
Do not run a provider, old baseline, new holdout or installed owner. Record exact source hashes, binary hash, commands/exit codes and resource snapshots in <taskroot>\hlp03-r2-source-qa.json plus a bounded QA log. Report residual limitations candidly. The coordinator freezes inputs and performs real evaluation after inspection.
