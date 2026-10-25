# AirWorker N-124 — bounded native rephase source milestone

LPR directive: 2026-10-25 20:04 SGT.
Existing node: N-124_p1-detect-cross-branch-node-id-collision-and-sta.
Owner: Dev Work, same existing AirWorker development owner. No parallel owner/task/node.

## Source baseline

- Canonical development ref: github refs/heads/dev/airworker-learning-loop-20261024.
- Exact baseline HEAD: 58bbca1b7ad568af31135e55d741496a2f764343.
- Canonical checkout contains preserved HLP-03 WIP; N124 source implementation is isolated in
  F:/-5-/011_Plugins/_worktrees/airworker-n124-rephase-20261025,
  branch dev/airworker-n124-rephase-20261025, from the exact baseline.
- No other active writer process was observed for the canonical checkout. Existing old worktrees are preserved.
- Installed AirWorker remains 0.11.10; no install/update is authorized.

## Required milestone

Implement one minimal source-only native operation for exactly one existing owned OPEN full-ID and one release generation. Reuse existing product lock, bounded receipt, stable request-id/idempotency journal, durable write/readback and rollback patterns.

The operation must require explicit external CAS:
- selected repo/remote identity,
- canonical full ref,
- exact Git HEAD,
- accepted ancestry,
- exact node/PLAN/header/release-slices SHA,
- exact authority receipt SHA,
- exact source/provenance evidence.

Allowed mutation is only parent/return_to/trigger/done_when on the named OPEN node, one exact PLAN header span, and one declared release step trigger/done_when. It must preserve id/title/owner/status/existing receipts and must never close the node.

No bulk import, renumbering, namespace merge, historical CLOSED transfer, universal migrator or remote Git publication.

## Required negatives

Wrong repo/ref, missing authority, stale HEAD/node/PLAN/header/release SHA, wrong ancestry, ambiguous/missing full-ID, non-OPEN/owner drift, forbidden status/owner/title change, >1 node/generation, request-id rebinding, malformed/oversized/symlink receipt, Unicode/path ambiguity, partial write/readback failure rollback and idempotent replay.

Frozen AirVera S3 expected HEAD f0241f52ce5d0929f1179dadc5f7be8bd95dedf8 must HOLD against a different fixture HEAD. Live AirVera PLAN/ref must not be mutated to satisfy the test.

## QA boundary

Use only F:-owned source worktree and F:-owned bounded QA scratch. No new heavy QA/cache on R:.
Targeted tests first; bounded cmd regression/build/vet if resource readback remains inside budget. Independent exact-SHA review after source freeze.

## Not authorized

AirWorker installation; native apply/rephase on AirVera; canonical Vera root/ref selection; trusted actor/generation designation; Git A3 publication/probe expansion; release/publish/install; third Vera judge; production/provider writes.


## Session checkpoint — 2026-10-25 20:37 SGT

Source implementation is now in an isolated same-owner worktree/branch from the exact canonical development HEAD:

- worktree: `F:/-5-/011_Plugins/_worktrees/airworker-n124-rephase-20261025/air-worker`;
- branch: `dev/airworker-n124-rephase-20261025`;
- baseline: `58bbca1b7ad568af31135e55d741496a2f764343`;
- canonical development ref `github/dev/airworker-learning-loop-20261024` still points to the same baseline;
- canonical checkout HLP-03 WIP remains preserved and untouched.

Current source candidate files:
- `cmd/planner.go` — wires `plan rephase`;
- `cmd/plan_rephase.go` — bounded one-node/one-generation source implementation;
- `cmd/plan_rephase_test.go` — positive, stale-CAS, malformed/bulk, rollback/readback and idempotency coverage.

Recovered no-blind-retry QA:
- targeted `TestN124PlanRephase*`: PASS; `airos/air-worker` 28.303s, tray package no matching tests;
- `go vet ./...`: PASS;
- `git diff --check`: PASS.

Checkpoint is intentionally **not** N124 RESULT_READY. Full regression, source freeze, machine acceptance receipt and independent exact-SHA judge remain for the next session. No AirVera native apply, trusted actor/ref selection, release, install, promotion or Git A3 publication is authorized.
