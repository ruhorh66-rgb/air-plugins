# HLP-03 R7: complete causal correction before placement

## Authorized scope

The user explicitly continued after the completed R6 checkpoint. Work only on SRVLM01 in the existing checkout, source base 68f9725c433df319ccb71624f9dce0d5f0d07b9e. This is one deliberately scoped candidate; no automatic R8, model sweep, fallback, selective retry or gold relabeling.

Change only the generic prompt in cmd/learn_adapter.go. Preserve installed runtime/plugin 0.11.10, Module Learning v0.1.1, services, configs, native PLAN/N/LEARN, foreign WIP, existing wrapper/tests/harness and all R1–R6 evidence. Never contact AIR-ENV-002; no new checkout/clone/stash/reset/clean or shared-cache cleanup. No release/tag/install.

## Evidence and bounded hypothesis

Read the immutable R6 evaluation and boundary diagnosis:
- docs/receipts/LEARNING_HLP03_REFINEMENT_R6_EVAL_20261024.json
- docs/receipts/LEARNING_HLP03_R6_BOUNDARY_DIAGNOSIS_20261024.json
- docs/receipts/LEARNING_HLP03_R6_INDEPENDENT_REVIEW_20261024.json

R6 full independent acceptance is23/24. C18 adds a protective boundary check to the old procedure but omits repair of the established cause. Frozen event, complete seed and actual captured native prompt were reconciled; the required correction reached the recorded request. The cause of the model's choice is unknown. The existing phrase requiring distinct correction is not explicit about retaining the full successful corrective operation before placement; its after-completion example also does not expressly cover a different operation that repairs an unmet precondition.

The hypothesis is that making those two obligations explicit will prevent reducing the lesson to a convenient local check. It is a hypothesis until measured, not a claimed fix.

## Exact mechanical source replacement

Replace exactly one occurrence of:

Decide coverage before placement.

with:

Identify the complete reusable correction supported by the run before deciding coverage and placement. When the evidence requires both a protective check and a repair of the established cause, retain both and choose placement for the whole correction, not for the check alone. A different corrective operation remains distinct when it repairs an unmet precondition, even if the existing operation has not completed.

Keep the rest of the prompt byte-for-byte except gofmt if needed. No domain/case names, expected answers, new examples, schema, validator, second provider pass, queue, database or classifier. The existing full-body preservation, eligible exact target/preSHA, freshness, lifecycle and already-completed-state requirements remain.

## Counterchecks

- C17 remains an extension of the same operation to a declared mode. A different applicability condition alone must not become CREATE.
- C20 is a useful early check inside the existing integrity contract. Because its evidence establishes no separate corrective repair, the new text must not invent one or replace mandatory full digest verification.
- C18 requires both guarding invalid input and the supported distinct repair; keeping just the check is incomplete.
- C02 and C15 retain the accepted same-operation extension and already-completed transformation behavior. All known24 are re-evaluated, not just C18.

## Reuse and execution

Hermes reuse remains the pinned existing-suitable-skill-first/full-current-read pattern recorded in F:/-5-/011_Plugins/AirWorker_Wiki/05_KNOWLEDGE_BASE/AIRWORKER_HERMES_REUSE_REVIEWER_20261024.json (Hermes commit0e427d788d253630d38a3a51cd76849e73cd6925). Use existing AirWorker verified APPLIED catalogue and Module Learning preSHA/backup/rollback. No new mechanism is justified.

Exact replacement, hashes, QA, freeze and scoring of structural facts are script work. No native author model call is needed for the mechanical edit. Host advisory text review is not counted as native source/model acceptance. Independent full-text judgment uses the existing native AirWorker executor gpt-6-sol/medium.

Run existing targeted Go tests/vet/build with the task-owned cache/temp. The PowerShell runner and focused tests are unchanged; reuse R6 evidence only after verifying their exact SHA and the prior QA/log SHA. No new PowerShell run is claimed. Do not add tests that merely match the new wording.

Before answers, freeze candidate, binary, protocol, unchanged known24 inputs/gold and the exact still-unexposed R5 fresh6 inputs/gold. Follow docs/tasks/HLP03_EVALUATION_PROTOCOL_20261024_R7.json:32 designated, one first attempt each. Known technical/class/identity failure or clear content violation stops later series. Old2 requires verbatim actual designated fresh updates frozen before calls. Missing prerequisites stay visible.

Resources: DEV-137 v1.1.0;2GiB monitored cache/temp working estimate, not OS quota; one native model at a time;1800s run/240s case;10GiB/10% reserve. No full DEV137, release or production-effect claim.

## Required result

Preserve original native results and terminals. Report source acceptance, executed measurement, semantic model quality, blocked cases, P2/P3 and production effect separately. An independent review must read all actual full texts against frozen gold and qualify the narrowest next step. Save source/Wiki checkpoint with explicit GitHub readback and unchanged WIP.
