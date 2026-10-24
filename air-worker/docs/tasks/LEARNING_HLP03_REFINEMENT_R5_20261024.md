# HLP-03 R5: bounded intermediate-release quality candidate

User direction on 2026-10-24: optimize the development and production plans toward the nearest intermediate release and continue. SRVLM01 only; existing checkout; base HEAD 92fd3ce9ad71f6ea5fe7c8252a341191ce4d6619.

## Scope and diagnosis

R4 source acceptance is ACCEPTED_WITH_LIMITS, model quality FAIL (31/32 class+identity, 29/32 conservative full), production effect NOT_PROVEN. The complete preserved answers show:
- C21: an existing integrity caveat did not teach the separate producer/schema remedy; reviewer returned no_candidate.
- C11: the old deadline, three attempts and same input were retained, but the new wait had no explicit pre-wait feasibility check.
- C15: this was create, not update; normalization was already complete but a new step repeated it. A no-repeat pitfall was present.

Add one generic paragraph to the existing production reviewer prompt, with no case IDs, domains, example answers, new schema or validator:

> Assess coverage by the corrective actions a procedure teaches, not by a caveat that merely names a limitation; a caveat alone does not supply a missing remedy. Keep a procedure's applicability and numbered actions consistent with established input state: do not require repeating a transformation that the evidence confirms is already complete. Before prescribing a new wait or action under an existing limit, require checking that it can fit within the remaining limit; if it cannot, preserve the original failure or stop outcome without taking the action.

The existing R3 causal-boundary and full-body preservation contract, R4 enum schema and final pair/freshness validators remain unchanged. No harness/runner/module/dependency/runtime/native PLAN/N/LEARN changes. Preserve all R1–R4 evidence and foreign WIP.

## Execution and acceptance

The coordinator supplied the bounded text; its application is a deterministic edit, followed by gofmt on the one changed Go file. No separate coding-model call is needed for applying that exact text.
- Unset AIR_WORKER_HLP03_LIVE for offline QA.
- Reuse task-owned GOCACHE, GOTMPDIR/TEMP/TMP and existing session-state directory.
- Run existing targeted Go tests: go test . -run '^(TestLearningReviewer|TestHLP03)' -count=1 -timeout 180s -json.
- Run go vet . and build a separate task-owned air-worker-hlp03-r5.exe.
- Record exact commands, exits, source/binary hashes and metadata before/after; resource estimate 2 GiB, not hard quota; shared volume reserve 10 GiB/10%.
- Existing R4 runner/schema/process-tree QA is reused because those files are unchanged; no new English substring test may claim semantic quality.

Before any R5 learning-reviewer response, freeze candidate hashes, protocol, known24 inputs/gold, fresh6 inputs/gold and two old-case followup designs/gold. One first attempt per case and no model fallback. The fresh series advances only if known24 technical integrity, cleanup and class/exact identity all pass. Failed advance gates leave remaining cases NOT_RUN_BLOCKED; they are never dropped from designated denominators. Actual updated bodies must be copied verbatim for followups, otherwise the prerequisite fails. A native independent gpt-6-sol / medium review assesses complete text and source separately. No automatic R6 or selective retries.

Source acceptance, model quality and installed delivery/natural effect remain separate. Release version is UNASSIGNED_NATIVE_REPHASE_PENDING. N124/P2 and final package/rollback gates are not waived. No release, installation, service changes, new N IDs, new checkout or AIR-ENV-002 access.
