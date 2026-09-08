# AirCoder ? Astra final acceptance checkpoint ? 2026-09-08

review_base: `eaf4b2e51bb7421ff29479ce3a24145f126a0f56`  
candidate_code_tip: `0a1ce69b53b67e817305d582904945309688efa7`  
version: `0.1.0-beta.3` development candidate

## Astra findings

- R1: **closed** ? resume rechecks scope/protected diff before acceptance; regression-covered.
- R2: **closed** ? diff quality covers staged + unstaged state relative to HEAD; staged whitespace regression-covered.
- R3: **closed** ? timing/result persistence survives resume and final-state rereads.
- R4: **closed** ? ordinary chat entry inferred routing facts, selected `native_cli -> codex`, live-probed Codex, materialized the task contract and completed through the bounded runner without user-supplied route/JSON.

## AC-04

Formal current registry: **4/5 accepted**, two products, false-ready=0.

| Task | Product | Initial SHA | Final/evidence SHA | Outcome | Receipt min | Native attempts | Repair |
|---|---|---|---|---|---:|---:|---:|
| 001 | air-coder | `3c53be4` | `cabcc54` | accepted 3/3 | 4.815 | 1 | 0 |
| 002 | air-ruflo-bridge | `a3f76af` | `bd9a293` | accepted 4/4 | 4.373 | 1 | 0 |
| 003B | air-coder | `a44d89e` | `9fbbf44` | accepted 3/3 | 7.749 | 1 | 0 |
| 004B | air-ruflo-bridge | `fc225a1` | `e831298` recovered after timeout | executor timeout | 12.252 | 1 | 0 |
| 005B | air-ruflo-bridge | `900aa82` | `fbed80c` | accepted 3/3 | 4.019 | 1 accepted retry | 0 |

Historical pilot burden is not erased: original AC04-003 and AC04-004 each consumed one quota-blocked native attempt (0.645 and 0.603 min), and the first 005B usage-limit attempt consumed 0.734 min. Across the current five tasks plus those preserved failed attempts: **8 native calls, 0 repair calls, 35.190 receipt-minutes**. Automatic paid retries after infrastructure failure: 0. Direct monetary cost is unknown/null in receipts; do not infer zero cost.

## Live resume

PASS. Initial SHA `67a4dc6`; exact native accepted diff SHA `86caead`. One executor run/thread was preserved across outer-runner interruption; final accepted 4/4; no second LLM turn. Active elapsed 446.076 s, resume wait 0.457 s.

## R4 ordinary-entry E2E

PASS. Red baseline `f868131`; native accepted fix `28897c0`; integrated candidate fix `0a1ce69`. Selector chose Codex from session-inferred ordinary implementation facts, Codex live probe passed, runner finished accepted 3/3 with one executor attempt and zero repair. Receipt elapsed: 8.090 min. No user route choice or task JSON was required.

## Economics / evidence limits

- Configured model for newer receipts: `gpt-5.6-sol`; events do not independently prove the effective model, so it remains labelled configured where appropriate.
- Direct cost: unknown/null for all measured runs.
- Manual returns in the five-task registry: 0.
- Comparable historical baseline for claimed prior 3?4 manual coding passes is not measured; no ROI percentage is claimed.
- Live-resume (7.435 min) and R4 E2E (8.090 min) are mechanism/integration checks and are reported separately from the five-task AC-04 acceptance economics.

## Release gate

Technical Astra gates R1?R4, AC-04 >=4/5, false-ready=0 and live-resume are closed. Remaining action is the explicit release decision: final exact-package regression/validation/installability proof, then merge/tag/production update/smoke only under the release authorization. Rollback target remains `air-coder--v0.1.0-beta.2` at `f7e9020146cca41015c1ed4832bb317f4126cd4b`.
