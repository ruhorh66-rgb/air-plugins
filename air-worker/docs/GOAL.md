# air-worker goal

plan_for_version: 0.10.5

## Goal

Make AirWorker the protected multi-session execution kernel used by AirBot-style hosts: several users, sessions and jobs may work on the same product concurrently without sharing mutable session state, locks, gates or job ownership.

## Architecture invariant

- AirVera, AirLegal, ASW and other products are consumers and evidence sources, not architecture owners.
- Every mutable runtime object is addressed by product + principal + session; jobs additionally use `job_id`.
- No global singleton lock may serialize unrelated working sessions.
- Machine-wide install, tray and version metadata may be shared only as host state, never as a work-session lock.
- A session may not close, restart, overwrite or claim another principal/session job or guard state.
- Cross-scope mutation fails closed with a reason.
- Running jobs are version-pinned for their lifetime; publishing a package for new sessions does not require killing an existing job.

## Critical path

1. Host-neutral principal/session identity (`39`).
2. Multi-session isolation core (`83`).
3. Durable owned job receipts (`73`).
4. Canonical PlanState with scoped runtime state (`71`).5. Binary protection hook and session-aware guard (`38`, `41`).
6. Safe one-work-step execution per session (`74`).

## Acceptance for 0.10.5

- Two sessions on the same product run concurrently with distinct session/job state.
- Finishing or failing one does not stop, close or overwrite the other.
- Duplicate-start protection is per owned job/scope, not global.
- Cross-session and cross-principal mutation fails closed.
- Protection hooks and guard resolve and act on the current session identity.
- No live consumer process must be killed merely to publish a new package for new sessions.

## Deferred after the first protected kernel

Distribution selfcheck, full validate/selector batching, cheap structured observability, subagent accounting/topology and role cleanup are the 0.10.6 hardening layer. GPT Chat + RDC remains the following external-host adapter layer.