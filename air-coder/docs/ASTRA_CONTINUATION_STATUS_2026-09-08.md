# AirCoder — continuation after Astra review

Date: 2026-09-08 21:25 +08
Branch: `ai/aircoder-ruflo-contract-beta3-20260907`
Candidate before this status commit: `dae81b3`
Release gate: CLOSED.

## Closed since Astra review

- R1/R2/R3: fixed and regression-covered.
- AC04-003B: accepted, 3/3, one executor attempt, zero repair; fix integrated.
- AC04-004B: executor timed out; preserved as a failed pilot result. Its minimal recovered diff independently passes the declared tests and is integrated as product hardening, but is not reclassified as accepted.
- R4 mechanical routing: plain implementation now selects ready native Codex by default; size alone does not force Ruflo; explicit substantial signs still route Ruflo; analysis/conserve remain ChatGPT+RDC.
- Integrated regression state: 51/51 AirCoder, AC04 bridge target PASS, bridge base 7/7, diff check PASS.

## Current AC-04 evidence

- Accepted: 001, 002, 003B = 3/5.
- 004B: executor timeout, not accepted.
- 005B: red baseline confirmed; latest run stopped as `executor_unavailable / usage_limit`, attempts=1, repair=0, changed_paths=0.
- Native Codex retry hint from receipt: after 23:02 local.
- False-ready: 0. Automatic paid retry after quota/timeout: 0.

## Remaining release blockers

1. Re-run 005B only when native quota is available; keep original red baseline and task contract unchanged.
2. Execute AC04-RESUME-001 live interruption/resume from clean SHA `67a4dc6`, requiring one executor attempt and no second LLM turn after restart.
3. Demonstrate ordinary chat entry end-to-end through the new R4 route when native executor is available.
4. Reconcile final AC04 registry/economics and require at least 4/5 accepted before AC-05.
5. Only then prepare merge/tag/install/smoke/rollback package.
