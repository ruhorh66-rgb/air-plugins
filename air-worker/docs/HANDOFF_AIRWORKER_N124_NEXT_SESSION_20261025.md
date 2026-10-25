# HANDOFF — AirWorker N-124 bounded native rephase

Date: 2026-10-25 20:37 SGT
Next session owner: **Dev Work / existing AirWorker development owner**
Node: `N-124_p1-detect-cross-branch-node-id-collision-and-sta`
State: **SOURCE WIP CHECKPOINT — continue, do not apply**

## 1. Exact continuation point

Source worktree:
`F:/-5-/011_Plugins/_worktrees/airworker-n124-rephase-20261025/air-worker`

Branch:
`dev/airworker-n124-rephase-20261025`

Exact source candidate commit:
`599e0bbeb7c78857cccfdf4b08bcdb1b61070a52`

Baseline parent:
`58bbca1b7ad568af31135e55d741496a2f764343`

Canonical development ref at handoff:
`github refs/heads/dev/airworker-learning-loop-20261024 = 58bbca1b7ad568af31135e55d741496a2f764343`

Do not reset/stash/clean the canonical HLP-03 checkout:
`R:/-4-/air-worker/worktrees/airworker-learning-loop-20261024/air-worker`

It contains preserved HLP-03 WIP unrelated to this N124 source slice.

## 2. Development / production plan state

Development continuation attachment committed in source candidate:
`docs/tasks/N124_BOUNDED_NATIVE_REPHASE_SOURCE_20261025.md`
SHA-256: `6fc64001b6aec929a1c3241bcd585822affcf220a38b451dd7aec0f0e8013882`

Production-gate continuation attachment committed in source candidate:
`docs/tasks/N124_PRODUCTION_PLAN_CONTINUATION_20261025.md`
SHA-256: `6c074b4d0440deea5a24fe0de7910baeb303def83a38ab893aa995361f706fa0`

Canonical AirWorker product-vault strategy files were **not** overwritten because the vault already contained pre-existing dirty HLP-03 edits:
- `00_STRATEGY/AIRWORKER_DEVELOPMENT_PLAN_INDEX.md`
- `00_STRATEGY/AIRWORKER_PRODUCTION_PLAN.md`
- `PRODUCT_INSTRUCTION.md`
- related N124/HLP03 WIP.

Next session must reconcile the committed N124 continuation attachments into the canonical product-vault plans only after preserving/identifying that existing dirty owner state. Do not blindly replace those files.

## 3. Current implementation

Committed source files:

- `cmd/planner.go` SHA-256 `4708b96c98179fcb261468f7ea979816bc2d99fbd1a9d62b7b57dd26655e156c` — dispatches `plan rephase`.
- `cmd/plan_rephase.go` SHA-256 `1706a2fcf522f0c4c308f2233522aeed050b6810c4afa0c1062db6875939b7b6` — bounded one OPEN full-ID + one release generation transaction.
- `cmd/plan_rephase_test.go` SHA-256 `4bedf1b24e0e28f5a9428e54bff2a60f3e8e0f7eeaeb128a58908d5f0477cfe0`.

Implemented boundaries include:

- strict request and authority schemas with exact byte SHA pins;
- exact repository/ref/HEAD/ancestor CAS;
- exact node/PLAN/header/release-slices CAS;
- existing product lock;
- stable request-id journal and idempotent replay;
- snapshot and rollback;
- durable node/PLAN/release writes plus readback;
- preservation of id/title/owner/status/existing receipts;
- public source command remains HOLD if trusted authority verifier is not configured;
- no remote Git publication in the native operation.

## 4. Tests already recovered

The last QA wrapper initially returned `EXECUTION_UNKNOWN`. It was **not rerun blindly**; the original owned process was found and its terminal output recovered.

Recovered result:

- `go test ./... -run TestN124PlanRephase -count=1` — PASS;
- package `airos/air-worker`: 28.303s;
- tray package: PASS, no matching tests;
- `go vet ./...` — PASS;
- `git diff --check` — PASS.

Owned temporary QA scratch was removed after the result was captured. No heavy new QA was left on R:.

## 5. Not completed

This checkpoint is **not** N124 RESULT_READY.

Still required:

1. review committed source against the bounded N124 spec;
2. bounded full cmd regression/build/vet using F:-owned GOCACHE/GOTMPDIR;
3. freeze final exact source SHA;
4. one independent exact-SHA review of AirWorker source;
5. fix any blocking finding in the same branch;
6. final machine source receipt and rollback receipt;
7. product-vault development/production plan reconciliation after preserving the dirty HLP-03 owner state.

N124 must remain OPEN unless its original complete done_when is actually satisfied through native AirWorker process.

## 6. Explicit HOLDs / not authorized

- AirWorker install/update;
- AirVera native apply/rephase;
- live Vera PLAN/N/LEARN/release-slices mutation;
- selecting canonical Vera root/ref/trusted actor/generation;
- release/promotion/publish;
- expanded Git/A3 publication gate;
- third Vera judge;
- mass import/renumbering/158-card merge/universal migrator.

Frozen S3 `f0241f52...` against a different selected/current HEAD must remain HOLD.

## 7. Start-of-session checks

1. Confirm branch tip and remote readback.
2. Confirm canonical HLP-03 checkout WIP still exists and was not mixed into this branch.
3. Confirm no second active writer on this N124 branch/worktree.
4. Read:
   - this handoff;
   - `docs/receipts/N124_SOURCE_CHECKPOINT_20261025.json`;
   - both N124 plan-continuation attachments;
   - original AirCurator N124 read-only spec if needed.
5. Continue source-only milestone only; do not perform native apply/install/release.
