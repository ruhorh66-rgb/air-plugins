# Goal — AirCoder 0.1.0-beta.3

plan_for_version: 0.1.0-beta.3
status: confirmed_by_lpr
confirmed: 2026-09-08

## Result

- было: AirCoder только выбирает ChatGPT+RDC, Ruflo или native CLI → стало: для обычной coding-задачи есть полный готовый путь через штатный Codex CLI;
- было: тесты/repair/resume зависели от дисциплины сессии → стало: thin runner механически выполняет context gate, diff/tests, bounded repair и persisted receipt;
- было: substantial Ruflo route можно было обойти ручным launcher → стало: Ruflo handoff закреплён только через canonical air-ruflo-bridge;
- было: инфраструктура coding loop росла как AIR-specific код → стало: executor/session принадлежат готовому Codex/Claude/Ruflo, AirCoder хранит только AIR policy/contracts/receipts.

## State facts required for beta.3

1. `product.json`, manifests, entry skill и contracts называют одну beta.3.
2. `run_coding_task.py` использует штатные `codex exec` / `codex exec resume`, без собственного agent loop.
3. Context/repository mismatch блокирует изменение до executor call.
4. Changed-path и protected-path gates проверяют diff независимо от самоотчёта executor.
5. Product checks запускаются автоматически; repair ограничен максимум двумя попытками.
6. Infrastructure failure не вызывает автоматический повтор платного LLM-вызова.
7. Interrupted in-flight state не replay-ится автоматически.
8. Runtime receipts живут вне Git в `AIR_CODER_RUN_ROOT`.
9. Ruflo route использует только `air-ruflo-bridge:run-via-ruflo` и full-loop preflight.

## Boundaries

Нет собственного planner/scheduler/watchdog/controller/queue/learning DB/LLM gateway. Нет переписывания Codex, Claude Code, Ruflo, AirWorker или LM Router. Executor не commit/push/merge/tag/release из bounded runner; эти действия остаются внешней AIR release-стадией.

## Acceptance sequence

1. Mechanical suite and schema/manifest validation PASS.
2. One real isolated self-hosting task passes `task → Codex → independent checks → result`.
3. Resume/failure paths remain bounded and fail-closed.
4. AC-04 pilot executes five comparable small tasks from at least two AIR products; at least 4/5 accepted with no false-ready result.
5. Only after pilot evidence: release package, rollback target, merge/tag/install/smoke.

## Existing release baseline

- beta.1 merged to `main` at `bb9f8a0`.
- beta.2 merged to `main` at `41793b5` and tagged `air-coder--v0.1.0-beta.2`.
- Codex `0.145.0` and Claude Code `2.1.226` are live-observed on SRVLM01; versions are observations, not permanent pins.
- beta.2 installed selector SHA parity was previously confirmed on both hosts.

## Beta.3 evidence to accumulate

- branch: `ai/aircoder-ruflo-contract-beta3-20260907`;
- Ruflo component assimilation: 35/35 recorded;
- Ruflo engine observed/preflighted at `3.38.21`, rollback runtime `3.38.11`;
- pre-pilot runner regression suite: 20/20 PASS on 2026-09-08;
- live Codex programmatic smoke: `thread.started`, `item.completed`, `turn.completed`, rc=0;
- first AC-03 real pilot: pending until this implementation packet is committed/pushed.

Beta.3 is `implemented_candidate` until the real pilot passes. It is not `pilot_passed`, `released` or `stable` merely because tests are green.
## Control gate — 2026-09-08

LPR decision: stop after the current approximately 20-minute implementation run,
produce a control slice, request Astra review, and only then continue development.

Current AC-04 state at the gate:
- 2/5 accepted across two products (`air-coder`, `air-ruflo-bridge`);
- 2/5 stopped as executor-unavailable because native Codex quota was exhausted;
- false-ready = 0; automatic paid retry on those failures = 0;
- AC04-005 red baseline is prepared and pushed but intentionally not executed;
- evidence: `docs/AC04_CONTROL_SLICE_2026-09-08.md`.

Release, merge, tag, install and AirStorage modernization remain after this review gate.

## Extended work window before Astra review - 2026-09-08

LPR decision: Astra review is temporarily unavailable because of its five-hour quota window. Continue productive AirCoder work for roughly one hour, but keep the review/release gate in place.

During this window:
- do manual hardening and prepare new regression-backed AC-04 tasks;
- do not spend native Codex calls while its own usage limit is exhausted;
- do not start release, merge, install, AirStorage modernization, or AIRVR;
- once Astra becomes available, provide the corrected control slice before release decisions.

Current integration baseline after manual hardening: `fd88acf058be637ee6d8e96918d3d396568617ee`.

## Astra review handoff — 2026-09-08 18:06 local

LPR decision: freeze development at the current control point and request Astra review before any further AC-04 execution or AC-05 release work.

Review package: `docs/ASTRA_REVIEW_SLICE_2026-09-08.md` + `.json`.
Release/merge/tag/install, AirStorage modernization and AIRVR remain blocked by this review gate unless LPR explicitly overrides it.

## Post-Astra continuation — 2026-09-08 21:25 local

Current continuation evidence is recorded in `docs/ASTRA_CONTINUATION_STATUS_2026-09-08.md`.

- R1/R2/R3 are closed and regression-covered.
- R4 mechanical routing is closed; live ordinary-chat E2E remains a release blocker.
- AC-04 accepted count is 3/5: 001, 002, 003B.
- 004B remains an honest timeout result although its independently verified fix is integrated.
- 005B is red and clean but native execution is quota-blocked; no automatic retry is allowed.
- AC04-RESUME-001 remains intentionally red and untouched until the live one-turn interruption/resume run.
- AC-05 release/merge/tag/install remains blocked until AC-04 reaches at least 4/5 and live resume passes.

## LPR-authorized self-execution — 2026-09-08 21:45 local

Because native Codex returned a usage-limit receipt, LPR explicitly authorized the current ChatGPT+RDC session to continue implementation itself rather than wait for native quota.

Development result:
- AC04-005B product defect fixed and fully regression-checked without altering its original Codex `executor_unavailable` receipt.
- Resume `task_id` path escape fixed before state-path construction and protected by a permanent regression.
- R1-R4 and all known AC-04 functional defects are now integrated in candidate `f7233a9f3f04bcff7ce6c1433aa641cd18670fcd`.
- Combined mechanical gate is green: AirCoder 52/52 plus bridge/listener AC-04 checks.

Acceptance accounting remains conservative: formal native-executor AC-04 score stays 3/5; manual self-execution evidence is tracked separately. AC-05 release materials may be prepared, but default-branch merge/tag/install remain gated until the final release decision.

## Astra final checkpoint ? 2026-09-08

- R1/R2/R3/R4: closed.
- AC-04: 4/5 accepted across two products; false-ready=0.
- Live interruption/resume: PASS with one executor turn and no replay.
- Ordinary chat -> selector -> Codex -> checks -> receipt: PASS.
- Final technical/economics evidence: `docs/ASTRA_FINAL_ACCEPTANCE_2026-09-08.md`.
- Merge/tag/production install remain behind the explicit release decision.
