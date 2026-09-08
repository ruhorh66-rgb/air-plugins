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

## Self-executor continuation — 2026-09-08 21:45 +08

LPR explicitly authorized this session to continue development as the coding executor while Codex native quota is unavailable.

- Current candidate: `f7233a9f3f04bcff7ce6c1433aa641cd18670fcd`.
- AC04-005B functional defect is fixed manually: isolated `17d94ace96c0cca57266472e578071ea5fb0ecf6`, integrated as `c14c487`; protected test PASS, listener verification 8/8, liveness 10/10, queue 7/7.
- Resume task-id escape is fixed manually: isolated `d22bc562f063938a1d39e53bbe550d3d053e4a96`, integrated as `4b3df94`, protected regression integrated as `b36585a`.
- Combined candidate verification: AirCoder 52/52 PASS; AC04-004B PASS; AC04-005B PASS; listener 8/8 + 10/10; queue 7/7; `task_id=".."` fail-closed; `git diff --check` PASS.
- Claude candidate plugin validation PASS and marketplace validation PASS.
- Installed production baseline remains `0.1.0-beta.2` in both Claude Code and Codex; no beta.3 install/update/tag/merge was performed.

Formal AC-04 executor evidence remains 3/5 accepted. Manual fixes do not rewrite the 004B timeout or 005B usage-limit receipts. Functional defects across all five AC-04 task cases are fixed and integrated; the remaining evidence gap is real native-executor/live-resume proof.
