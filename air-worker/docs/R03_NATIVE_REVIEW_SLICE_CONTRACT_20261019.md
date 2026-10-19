# AirWorker 0.11.9 · R03 native reviewer, applied checkpoint and release transition

**Scope:** source-only Worker B, pinned published [air-modules v0.1.0](https://github.com/ruhorh66-rgb/air-modules/tree/v0.1.0); no module fork, new module API, v0.1.1, release or install. Native `PLAN.md`, `plan/N-*`, `plan/release-slices.json` and LEARN belong to the designated sequential PLAN writer, not Worker B.

## Approved correction / meaning of R03

LPR's October 19 direction retains the published module v0.1.0 for the first AirWorker 0.11.9 micro-release. R03 is a **historical, verified checkpoint**: the original native failure was signed; the actual production reviewer returned a safe procedure; the module auto-applied that exact candidate; its immutable review/ledger/skill receipts and SHA matched **at the instant of core verification**. R03 is NOT a standing permission, a claim that a procedure will remain applied forever, or a `procedure_used`/full validation PASS.

The earlier draft wrongly made a new public module-to-PLAN atomic callback mandatory. That callback does not exist in v0.1.0 and is **not required for this bounded historical checkpoint**. We must not invent a module operation, borrow its private Windows mutex, fabricate `observe_verified_use`, or edit module data. A future stronger, continuously-current cross-store transaction is separate scope and is NOT a release-0.11.9 prerequisite.

## Trigger and exact evidence

- Active source release **0.11.9**, R02 **CLOSED**, R03 **OPEN**, R04 **PLANNED**; release-plan bytes must reconstruct exactly the canonical pre-R02 plan. R02 original capture receipt is immutable and belongs to the **same candidate executable SHA**.
- The **outer R02 release capture envelope must itself be signed by the trusted native AirWorker kernel** using the existing product-private HMAC key and domain separation. It binds exact owner/product root, original run ID/capture+replay SHA, current candidate executable SHA and exact pre-R02 release-plan SHA, including time and result. A signed *inner* CLI failure alone does not authenticate an outer release envelope: reject unsigned or changed envelopes before selecting a reviewer or attempting either R02 or R03 transition. No new air-modules API is required.
- Before **any first R02 child validation**, the source-owned `release-slice-r02-pending.json` must be sealed with a separate domain-bound native HMAC (`r02-pending/v2`) over owner/product, exact current executable SHA, release-plan SHA, disposable target, original baseline receipt inventory and start timestamp. Recovery reads the immutable same attempt and verifies its MAC BEFORE selecting prior CLI evidence; unsigned v1, modified plan/binary/target/baseline, external hardlink or duplicate JSON must fail closed. Never auto-upgrade or re-sign an existing pending marker; no foreign target cleanup under an unauthenticated marker.
- An exact original signed AirWorker CLI failure receipt plus **one** module-owned `run_completed` event, HMAC-backed invocation/source/principal/run, no manual LEARN injection.
- The module's actual production reviewer, one retry-safe RV, signed source-owned model-response attestation, canonical reviewer event-index, identical canonical RV bytes, exact candidate digest, source run/event/proposal/review IDs.
- **One** module-owned APPLIED ledger with live target `skills/learned/...@SHA`, current skill index, safe compiled action and Markdown schema. Blocking/controlling or unregistered action remains **PENDING_LPR**.
- An immutable native R03 receipt binds the original R02 SHA, verified candidate binary and before-plan SHA, model-response/completion attestation SHA, canonical RV/proposal and current managed target.
- The release-slices native kernel (one `release-slice-run` runner + `release-slices` plan writer, exact byte/CAS recheck, atomic durable file publication) changes ONLY `R03 OPEN -> CLOSED` and `R04 PLANNED -> OPEN`. No legacy numeric PLAN fallback or edits of `PLAN.md`/`plan/N-*`/LEARN.
- A crash after an immutable R03 receipt can recover by revalidating it **against the current module evidence** without re-running the reviewer. A missing, stale, competing, orphan or errored RV never starts another blind model call.
- `loop -whatif` and `loop -plan-only` have **no supported read-only release-slice preview**. Both fail closed (RC2) before dispatching R02/R03; they must not invoke reviewers, create receipts, use LEARN or modify any release-plan bytes. No numeric legacy PLAN fallback.

## Concurrency and security boundary

The native `release-slices` lock serializes **plan writers**, while published `learning.Execute` owns its **module** mutation locks. Those are intentionally distinct. Between the final module-read and PLAN CAS, a different authorized module rollback can occur. We must therefore make the following explicitly true:

1. R03's outcome is a **past, causally verified safe application**, not a continuously valid capability. `R03 CLOSED` alone is not proof that a procedure remains active.
2. The next real native `R04` handler must independently and freshly recheck current ledger/index/current target@SHA and native `skill_loaded` plus exactly one `procedure_used` and causal outcome/effect through the module's existing verified-use contract, or **fail closed**. No handler for R04 is enabled until this is demonstrated.
3. If rollback occurs **before** the R03 verification, promotion fails and the plan stays R03 OPEN; no release receipt may be minted from stale evidence.
4. If rollback occurs **after** the historical R03 checkpoint, the checkpoint may remain closed as an audit fact, but R04 cannot claim real use or advance release/QA on its basis. Never claim successful full validation or active procedure after such rollback.
5. To reduce the temporal race, native R03 rechecks the actual module evidence immediately before atomic status write and again at its reporting boundary. A later rollback is always possible; this is why R04 must remain a separate verified gate.

## Test and acceptance

- Positive: exact signed native R02 -> real module reviewer/APPLIED/target SHA -> immutable R03 receipt -> R03 CLOSED / R04 OPEN; replay must **not** create a second reviewer, receipt, event or transition.
- Negative: missing/stale outer R02 HMAC; altered or unsigned pending attempt under a new candidate (including pre-existing baseline receipt list, product/plan/exe/target); external NTFS pending hardlink, duplicated marker JSON; `-whatif`/`-plan-only` with R02 and R03 active must produce zero runner/model calls. Incorrect executable or plan SHA, altered canonical RV/model HMAC/candidate digest/target, unsafe rule, duplicate run/event, orphan reviewer, stale or malformed receipt, tampered plan, concurrent writer, wrong owner, rollback before verification also fail closed before success.
- Race: rollback after R03 is an **invalidation of later use**, not a fabricated `procedure_used`. Check R04 execution is unsupported until its independent current-use gate is accepted.
- QA: full Go tests/vet/build, source SHA frozen, independent `gpt-6-astra/high` PASS P0/P1=0 with exact receipts and Git readback. Source-only handler is not permission to promote the live release plan.
- Published v0.1.0 and installed AirWorker 0.11.8 unchanged. Tag/release/marketplace/install/UAC/reboot/service-stop require their own exact LPR gates.

**Rollback:** isolate any failed candidate, keep installed 0.11.8. No operational rollback of a shared module or foreign service is performed in source QA.
