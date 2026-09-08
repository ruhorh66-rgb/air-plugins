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