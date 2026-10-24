# HLP-03 R6 preparation: failure diagnostics and explicit coverage/placement hypothesis

## Scope and authorization

The user asked to continue the current saved R5 checkpoint. Work only on SRVLM01 in this existing product checkout, base HEAD e6c3c7f960208b49bdd0b4e694bc7eb99554bdce. Use existing AirWorker/Hermes mechanisms. This task implements the next evidence-based source hypothesis and bounded measurement diagnostics. It does NOT run a learning reviewer or declare model-quality acceptance.

Only permitted edits:
1. cmd/learn_adapter.go (exact generic prompt changes below)
2. docs/tasks/run-hlp03-real-reviewer-20261024.ps1 (bounded error diagnostics only)
3. docs/tasks/test-run-hlp03-real-reviewer-20261024.ps1 (focused deterministic error-record tests)
4. docs/receipts/LEARNING_HLP03_R6_AUTHOR_20261024.json (your honest author/validation receipt)

Do not modify any other file. Do not edit historical R1–R5 evidence, corpora, gold, candidate snapshots, native PLAN/N/LEARN, configs, dependency pins, installed files or services. Preserve foreign WIP; no checkout/clone/stash/reset/commit/push, no AIR-ENV-002. Do not run Go/model/provider jobs or invoke another model. Do not call the full live runner. Do not change process ownership/termination behavior, run budgets, source guard conditions, retry policy, schemas/validators or module lifecycle.

The coordinator will run the complete focused tests on Windows PowerShell5.1 and Go QA with task-owned cache/temp. You may parse PowerShell and Go syntax and run purely in-process diagnostic helper checks. Existing PowerShell tests contain an actual child process: leave it intact for coordinator testing, do not launch or kill processes yourself. Report exactly what you validated.

## Evidence and hypothesis

Read docs/receipts/LEARNING_HLP03_REFINEMENT_R5_EVAL_20261024.json and docs/receipts/LEARNING_HLP03_R5_BOUNDARY_DIAGNOSIS_20261024.json, plus the current runner and test.
R5: known24 class/identity22, full21; false splits C02/C17 and content inconsistency C15. C11 wait feasibility and C21 schema distinction now correct. These are first-attempt observations, not proof that a particular prompt sentence caused a change. Keep current source and model acceptance separate.

### Exact prompt edits

A. In the existing Compare the trusted APPLIED procedures paragraph, retain the opening sentence, then replace text from "If an appropriate procedure already covers the lesson" through "Being in the same workflow or operating on the same object is not sufficient reason to update." with:

Decide coverage before placement. A caveat naming a limitation does not teach its remedy. If a procedure already teaches the required correction, return apply=false. Otherwise, update an eligible procedure when the addition extends its taught corrective operation to another condition, input form or mode, preserving its old branches. An old branch continuing to work is not evidence that the addition belongs in a new procedure. Create a separate procedure only when the required corrective operation is distinct, including a different failure remaining after the existing correction is complete for the current case. Compare the taught corrective operation, not merely a shared workflow, object, title or event class.

B. Replace the later sentence "Truly separate causes or corrective actions require a distinct procedure with both update fields empty." with:
For a distinct procedure, leave both update fields empty.

C. Replace the entire added R5 Assess coverage paragraph with:
Match the numbered actions to the applicability's starting state. Treat a transformation already confirmed complete as a precondition, and begin the procedure with the corrective action still required. Before prescribing a new wait or action under an existing limit, require checking that it can fit within the remaining limit; if it cannot, preserve the original failure or stop outcome without taking the action.

Retain exact eligible identity, full-body preservation, forbidden weakening, schema, freshness and validator behavior. No domain names, case IDs or expected answers in runtime prompt.

## Wrapper diagnostics

The R5 original terminal has Win32Exception and durable Goexit0, but no error position or stack. The nearby disappearing-directory warning does not establish the fatal cause. Do not implement a speculative behavioral fix or rewrite old evidence.

Extend the existing failure receipt additively with bounded diagnostic fields sufficient to locate a future failure:
- stable phase/operation at the failing observation, including distinguish disk query, cache/temp/evidence enumeration, append sample, wait/exit observation, protected/frozen hash readback, terminal serialization/write
- ErrorRecord fully-qualified id, exception type/HResult and native error code when available
- InvocationInfo command, script line/offset and bounded PositionMessage; bounded ScriptStackTrace

Use one small existing-runner helper if it makes faithful testing possible. Capture original ErrorRecord/diagnostic data before owned-child cleanup can change context. Set the phase/operation at actual execution boundaries, including inside Save-AwSample. Keep original status/error/go exit fields and schema compatibility. Do not turn failure into success, suppress a fatal error, change Stop-AwOwnedGo, broaden process searches, rerun samples after failure, or infer missing data.

Bound added string details (e.g. 4096 characters each); avoid dumping whole Process/ErrorRecord objects or environment. Null unavailable native error code is valid; do not invent one.

## Meaningful focused tests

Extend the existing AST-extracted helper test style, with no new test framework and no live-provider call. Generate a real caught ErrorRecord from a deterministic throw in a function, including a Win32Exception with known native error code. Verify real error location/stack, original phase/operation and native metadata survive JSON serialization/deserialization. Verify unavailable native code and bounded overlong details where practical. A mocked phase/stack constant alone is not evidence of capturing a real failure.
Preserve the existing complete/incomplete footprint and owned-child tests. Fixture cleanup must remain task-owned; do not delete shared caches or historical directories.

## Output

Write docs/receipts/LEARNING_HLP03_R6_AUTHOR_20261024.json (UTF-8 LF) with schema air-worker.hlp03-r6-author/v1, source_base_sha, exact files changed and SHA256, implementation summary, actual validations, limitations, source/model/production acceptance separation. Declare model quality NOT_MEASURED, production effect NOT_PROVEN. The coordinator will bind your completed native executor receipt afterward; do not invent its status/hash.
