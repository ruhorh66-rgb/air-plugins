# AirWorker handoff перед reboot SRVLM01 — 2026-09-16

## Каноническое состояние

- Live AirWorker: **0.10.5**.
- Release commit: `ccf5fb2457378cdb34186b72ad4e8c33d99f1e55` (`ccf5fb2`).
- Tag: `air-worker--v0.10.5`; origin/main и peeled tag указывают на тот же commit.
- Release SHA-256 CLI: `4DF6D36548529FE452E89515CE659CB810F5B0955267A5FC9DA65BB453A8F37C`.
- Claude cache 0.10.5, Codex cache 0.10.5, live install — тот же SHA.
- Live install: `C:\Users\admin_loc\AppData\Local\air-worker`.
- `air-worker install -status`: **нарушений источника нет**.

## 0.10.5 — что доказано

- `runner kind=router` работает через OpenCode + AirLLMRouter v0.3.0.
- Live OpenRouter free acceptance: real `read/edit/bash`, fixture `AFTER`, verifier `VERIFY_OK`, `cost=0`.
- Factual: `STEP PASS: К64, К65, К66`.
- Codex standalone semantic judge: `PASS`, `step_done=yes`, `drift=none`.
- Packaged OpenCode integrity/install test PASS; full Go test/vet/diff/plugin/version gate PASS.
- OpenCode 1.18.31 shipped pinned; archive/exe SHA и `--version` проверяются installer до и после install.
## Tools после штатного обновления

- Claude Code: `2.1.272` — PATH OK.
- Codex CLI: `0.145.0` — PATH OK.
- OpenCode: `1.18.31` — `air-worker tool -which opencode` PASS.
- AirLLMRouter: `0.3.0` — version probe PASS.
- Router bridge: установлен, 6201 байт.
- Активная ladder: `script → router → haiku:medium → haiku:max → sonnet:medium → sonnet:max → opus:medium → opus:max`.
- Известный diagnostic gap: `air-worker tool -which router` ищет фиктивный `router.exe` и даёт exit 2. Production runner не сломан; это новый К70 / шаг 86.

## Оптимизированный release train после reboot

1. **0.10.6 Operational feedback intake** — шаги 85, 86, 86а.
2. **0.10.7 Multi-session protection kernel** — 39/73/71/38/41/46/74/83 integration/acceptance.
3. **0.10.8 Operational hardening + observability** — 79/72/75/40/49/61.
4. **0.10.9 GPT Chat + RDC** — шаг 69.
5. **0.10.10 Semantic control completion** — шаг 68.
6. **0.11.0 Binary architecture cleanup** — прежний cleanup train.

Причина перестановки: feedback уже согласован ЛПР «в следующем релизе» и нужен всем эксплуатационным сессиям; смешивать его с большим multi-session kernel невыгодно по scope/risk/time-to-release.
## Шаг 85 — согласованный feedback contract

Эксплуатационная команда типа «сделай backlog / дай feedback разработчикам» должна приводить к одной операции:

`feedback_id → tracked evidence record + candidate в canonical PLAN`.

Минимальные поля: product, source_version, type, severity, observed, expected, evidence, reproduction, workaround, proposed_outcome. Эксплуатационная сессия описывает наблюдение и evidence, но не обязана проектировать реализацию.

`candidate` не является work-step, не меняет distance и не получает priority/release автоматически. Active scope меняется только решением ЛПР. Dual-write должен быть атомарным по смыслу: полный успех либо честный `PARTIAL`/ненулевой код с названием непроставленной стороны.

Общий стандарт нужно записать в AIR VIBE CODING thin router и DEV-030/040/080; это часть шага 85, а не отдельный параллельный план.

## Первый ход новой сессии

1. Прочитать этот handoff и `PLAN.md`.
2. `air-worker version` и `air-worker install -status` — подтвердить 0.10.5/OpenCode/bridge после reboot.
3. `git -C F:\-7- log -3 --oneline -- air-worker` и `git status --short`.
4. `air-worker goals -product F:\-7-\air-worker`; полный `report` не гонять без необходимости — factual judge остаётся дорогим.
5. Начать **шаг 85**, затем 86, затем 86а / release 0.10.6. Не повторять acceptance 0.10.5 без признака регрессии.
## Границы / не трогать

На `F:\-7-` остаются чужие dirty paths: `air-ruflo-bridge/.../run_task.ps1`, его `.bak`, `_worktrees/`, `site-builder/...`. Они не относятся к AirWorker и не должны попадать в его commits/reset/cleanup.

Перед reboot активных diagnostic `opencode/go/python router` процессов не найдено. Жив только штатный `air-worker-tray.exe` из live 0.10.5; reboot завершит его штатно.

## Cleanup этой сессии

Удалено 30 одноразовых 0.10.5 smoke/probe/release helpers из `.woody` и локальный `.tools`; ключевые release/K65/semantic логи перед удалением скопированы в `R:\-4-\air-worker\state`. Сохранены `.woody/jobs`, `goal-drift.jsonl`, `semantic-history.jsonl`, `semantic-verdict.json`, `recovery` и task/recovery evidence прошлых фаз.

PLAN обновлён под post-release состояние и проверен `air-worker goals`: **7 целей / 67 критериев, план годен**.
