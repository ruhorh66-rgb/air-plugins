# HLP-03 R3 — preserve the causal boundary between procedures

Authorized continuation from Ruhor on 2026-10-24: recheck and continue the plan. SRVLM01 only, existing checkout R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker, base HEAD 2524960bdb950bc075bb7cbebccbcf9bf933efb8. Use the installed Ponytail full-mode instructions and native AirWorker kernel. No new checkout, dependencies, services or framework.

R2's generic permission to refine within the same workflow caused false merges between separate causes. Its full-replacement preservation guidance must stay. R1 and R2 evidence, frozen corpora and snapshots are immutable. Do not inspect R3 corpus/gold/follow-up data or invoke the learning reviewer. The coordinator performs the later frozen measurement.

Only production source mutation allowed: cmd/learn_adapter.go, only the generic prompt paragraph inside buildLearningReviewerPromptWithSettings. Replace this R2 sentence:
"A useful additional input variant, conditional branch, or missing step in the same workflow can refine that procedure; a different detail alone does not require a new skill."
with this generic causal rule:
"A new condition or step may refine an existing procedure only when it directly completes or specializes the same causal correction, with prior applicability and actions preserved. If the existing correction already works and the remaining failure has a different cause and remedy, create a separate procedure. Being in the same workflow or operating on the same object is not sufficient reason to update."

Keep all surrounding rules, exact eligible target/preSHA, full replacement, old applicability/bounds/inputs/ordering/pitfalls preservation, untrusted data boundary, schema validators and native provider path unchanged. No corpus names, IDs or answer examples in the production prompt. Do not change harness, runner, module pin, native PLAN/N/LEARN or installed runtime/config. Preserve all unrelated WIP.

Validation: one owned QA at a time with inherited task GOCACHE/GOTMPDIR/TEMP/TMP. DEV-137 v1.1.0 uses 2 GiB monitored working estimate, not an OS quota. Do not clean shared cache. Record before/after cache/temp metadata.
- gofmt cmd/learn_adapter.go.
- With AIR_WORKER_HLP03_LIVE unset, from cmd: go test . -run '^(TestLearningReviewer|TestHLP03)' -count=1 -timeout 180s.
- go vet .
- go build -o <taskroot>\air-worker-hlp03-r3.exe .
- git diff --check for your source file.
Do not add literal-English tests that pretend to prove model semantics. Source tests do not establish model quality.

Write <taskroot>\hlp03-r3-source-qa.json with actual commands/exit codes, adapter/harness/runner/binary SHA-256 and bounded log paths, plus metadata. Read back and report. Taskroot is <product>\.air-worker\learning-loop-20261024. Never overwrite existing binaries/evidence; if R3 binary/receipt already exists, stop and reconcile rather than replace.
No commit/push/release/install/service changes or AIR-ENV-002. Coordinator handles source acceptance, real model quality and production effect separately.
