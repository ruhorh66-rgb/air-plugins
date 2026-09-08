# AC-04 control slice — 2026-09-08

status: review_gate
branch: `ai/aircoder-ruflo-contract-beta3-20260907`
integration_head: `737d69d5c8dd21964f1ed0108c4877884ab6cc7c`

## Acceptance target

Astra AC-04 requires five comparable small coding tasks from at least two AIR products,
with at least 4/5 accepted and no false-ready result. This slice intentionally stops
before release so Astra can review the observed mechanism and economics.

## Completed runs

| Task | Product | Result | Acceptance | Elapsed | Attempts | Repair |
|---|---|---|---:|---:|---:|---:|
| AC04-001 | air-coder | accepted | 3/3 | 4.815 min | 1 | 0 |
| AC04-002 | air-ruflo-bridge | accepted | 4/4 | 4.373 min | 1 | 0 |
| AC04-003 | air-ruflo-bridge | executor unavailable | 0/3 | 0.645 min | 1 | 0 |
| AC04-004 | air-coder | executor unavailable | 0/3 | 0.603 min | 1 | 0 |

## Evidence and observed behavior

- AC04-001 changed only `run_coding_task.py`; independent AirCoder suite: 28/28 PASS.
- AC04-002 changed only `approve_via_telegram.py`; status/signature/queue gates PASS.
- AC04-003 and AC04-004 produced no diff. Codex returned `usage limit` and `turn.failed`.
- Both infrastructure failures stopped after one executor call: no automatic paid retry.
- Neither infrastructure failure was reported as accepted: false-ready count remains zero.
- Native Codex reported the next usage window as 17:46 local time on SRVLM01.

## Prepared fifth task

`AIRCODER-AC04-005` is prepared but not executed into an exhausted quota window.
Its pushed red baseline is `3b126122daf07f4b1820443efe92ad4586b21ea2` and proves that a non-object
`limits` value is currently accepted although `coding-task.schema.json` requires an object.

## Review gate

Next action is Astra review of this control slice. Do not start beta.3 release, merge,
tag, installation, or AirStorage modernization before that review. After review, continue
AC-04 from the remaining tasks or adjust the route only if the review explicitly accepts it.
