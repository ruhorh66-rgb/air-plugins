# AC-04 control slice — 2026-09-08

status: review_gate_deferred; release_gate_closed
branch: `ai/aircoder-ruflo-contract-beta3-20260907`
integration_code_head: `fd88acf058be637ee6d8e96918d3d396568617ee`

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

## Original prepared fifth task — superseded by manual hardening

`AIRCODER-AC04-005` was prepared during the first slice but was later closed manually as schema hardening. Its red baseline remains historical evidence only; it is not counted as an accepted AC-04 pilot and must not be run as a fresh task.

## Review gate

Astra review remains mandatory before beta.3 release, merge, tag, installation, or AirStorage modernization. LPR deferred the review only because Astra is temporarily quota-limited and explicitly allowed continued AirCoder hardening in the meantime. Continue AC-04 preparation/work, but keep the release gate closed until that review.

## Post-slice corrections and hardening

After the first control gate, LPR kept the session working while Astra review was unavailable due quota limits. No release/merge/tag/install or AirStorage work was started.

Corrections:
- The earlier suspicion behind the AC04-003 listener baseline was a false positive in the test harness: queue `_push_next()` housekeeping was counted as Ruflo launch. Commit `4e63993` isolates the actual `RUN_TASK` assertion; listener integrity is now 8/8 PASS.
- AC04-003 and AC04-004 executor calls themselves still remain valid `executor unavailable` observations: both stopped before any diff because Codex native quota was exhausted.
- Commit `418c358` now classifies this condition as `executor_unavailable` with `reason=usage_limit` and `retry_after_hint`, with no automatic retry.
- `require_clean_start` and top-level `limits` type gaps were closed manually, not counted as AC-04 accepted pilots.
- Commit `fd88acf` aligns runtime top-level task validation with `coding-task.schema.json`; AirCoder suite is 35/35 PASS.

Replacement red baselines prepared for the next native window:
- `AC04-003B` AirCoder malformed inner limit values: `a44d89ec71f4613104518dda46116fd673b6e541`.
- `AC04-004B` bridge queue invalid `created_at`: `fc225a127c9a4277d46d75d7eba22bfd01b120f4`.
- `AC04-005B` bridge listener non-object approval request: `900aa82df54c7414e6c4e640bf2f21c541659f79`.

These branches contain protected regression tests only; product fixes are intentionally absent so they remain valid AC-04 coding pilots after native quota recovers.
