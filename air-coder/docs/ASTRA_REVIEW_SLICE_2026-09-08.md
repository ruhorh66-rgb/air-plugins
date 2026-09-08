# Astra review slice — AirCoder 0.1.0-beta.3 candidate

Status: READY_FOR_ASTRA_REVIEW
Date: 2026-09-08
Branch: `ai/aircoder-ruflo-contract-beta3-20260907`
Review code baseline: `0e658f80ca704cd519b0e520e91bd7c96e5239b1`
Release gate: CLOSED pending Astra review and AC-04 completion.

## What Astra should review

1. AC-01 passport and environment evidence in `ASTRA_AC01_AC02_BASELINE_2026-09-08.md`.
2. AC-02 choice of Codex CLI as primary ready executor and thin-adapter boundary.
3. AC-03 bounded executor path: context gate -> Codex exec/resume -> diff/protected gate -> checks -> <=2 repair -> persisted receipt.
4. AC-04 mechanism evidence, five-task registry, quota handling, prepared replacement pilots, and live-resume check plan.
5. Whether current evidence is sufficient to continue AC-04; release/merge/tag/install remain explicitly out of scope for this review point.

## Confirmed state

- AirCoder candidate: `0.1.0-beta.3`, development channel.
- Codex CLI: `0.145.0`; Claude Code observed: `2.1.226`.
- AIR_START: `0.14`; Core: `4.1.4`; AIR_VIBECODING observed now: `2.0.0-rc.6`.
- AC-03 self-hosting pilot: PASS, one executor turn, zero repair, independent checks PASS.
- Current AirCoder regression suite after resume hardening: 37/37 PASS.

## AC-04 current pilot state

- `AIRCODER-AC04-001` / AirCoder: ACCEPTED, 3/3, 4.815 min, attempts=1, repair=0.
- `AIRCODER-AC04-002` / air-ruflo-bridge: ACCEPTED, 4/4, 4.373 min, attempts=1, repair=0.
- Native quota exhaustion was correctly classified as `executor_unavailable / usage_limit`; automatic retry=0; false-ready=0.
- Official accepted count remains `2/5`; quota-stopped observations are not counted as product failures.
- Five-task canonical registry: `AC04_TASK_REGISTRY_2026-09-08.json`.

Prepared red baselines, fixes intentionally absent:
- `AC04-003B` AirCoder malformed inner limits: `a44d89ec71f4613104518dda46116fd673b6e541`.
- `AC04-004B` bridge invalid queue `created_at`: `fc225a127c9a4277d46d75d7eba22bfd01b120f4`.
- `AC04-005B` bridge non-object approval request: `900aa82df54c7414e6c4e640bf2f21c541659f79`.

All three task contracts exist under `E:/-4-/air-coder/pilot-tasks/` and passed local context/repo/HEAD/clean preflight before executor invocation.

## Resume evidence

- Resume revalidates context, repository, expected HEAD and origin while allowing the expected dirty diff left by a completed executor step.
- Unit evidence proves continuation can finish acceptance without a second `invoke_codex`; external HEAD drift blocks continuation.
- Live interruption/resume scenario is prepared in `AC04_RESUME_CHECK_2026-09-08.md`.
- Its isolated red baseline is `67a4dc6e596fab2f4a392613a97c30314463a372`; target defect is unsafe resume `task_id` path handling.

## Corrections since the first control slice

- Earlier suspicion that listener integrity allowed a rewritten objective to launch was false-positive test logic. Queue housekeeping `_push_next()` had been counted as launch. The corrected listener integrity suite is 8/8 PASS.
- Top-level task-schema/runtime type drift was manually hardened and is not counted as AC-04 accepted pilot work.
- A stale read-only Codex smoke process from 14:18 was terminated; no AirCoder/Codex runner remained active afterward.
- Native Codex retry window reopened at 17:46 local, but no new executor pilot was started before this review slice, per LPR instruction to freeze and request Astra review.

## Review questions

1. Is AC-03 architecture compliant with the Astra constraint to adapt a ready executor rather than build a planner/agent loop?
2. Is `executor_unavailable / usage_limit` with zero automatic retry the correct infrastructure-failure classification?
3. Are `003B/004B/005B` valid comparable remaining AC-04 pilot tasks and is the five-task registry sufficient?
4. Does the prepared live interruption/resume check satisfy AC-04.4 once executed, or does Astra require additional evidence?
5. What concrete blockers must be closed before AC-05 release preparation?

## Do not do before review

Do not merge, tag, release, install beta.3, start AirStorage modernization, or restart AIRVR. Continue only after Astra review or explicit LPR override.
