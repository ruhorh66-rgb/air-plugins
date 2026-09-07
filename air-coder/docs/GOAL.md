# Goal — AirCoder 0.1.0-beta.2

plan_for_version: 0.1.0-beta.2
status: confirmed_by_lpr
confirmed: 2026-09-06

## Result

- было: coding-задачи вручную разводятся между ChatGPT+RDC, Ruflo и native CLI → стало: один AirCoder entrypoint выдаёт проверяемый route и причину;
- было: выбор исполнителя сравнивается по впечатлению → стало: каждый реальный прогон имеет общий contract цены подтверждённого результата;
- было: новый selector рискует стать вторым orchestration stack → стало: продукт механически ограничен handoff к уже существующим executors.

## State facts

1. `air-coder/product.json` и один entry skill существуют.
2. Selector воспроизводимо выдаёт `chatgpt_rdc`, `ruflo`, `native_cli` на разных входах.
3. Claude/Codex manifests имеют одну version.
4. Run-result contract содержит acceptance/time/cost/quota/model/attempts/evidence.
5. Marketplace указывает `./air-coder`.
6. Existing `air-worker` и `air-ruflo-bridge` не изменяются этой задачей.

## Boundaries

Нет scheduler/watchdog/controller/queue/learning DB/LLM gateway. Нет рефакторинга AirWorker. Нет изменения Ruflo runtime. Нет merge в default без отдельного гейта.

## Delivery state — 2026-09-06

- beta.1 is merged to `main` at `bb9f8a0`;
- Codex 0.145.0 and Claude Code 2.1.226 both install and enable AirCoder from the candidate `air-plugins` marketplace;
- installed-cache selector smoke passes on both hosts;
- shared marketplace delivery is blocked only by the pre-existing invalid `_moved` tombstone; the fix removes it from the machine-readable plugin list and preserves the history in `.claude-plugin/MOVED_PLUGINS.md`;
- no AirWorker or Ruflo runtime change is part of this delivery fix.

## Beta.2 Windows input hardening

- Accept UTF-8 BOM in task-facts JSON (`utf-8-sig`), matching Windows PowerShell output.
- Regression test covers BOM input explicitly.
- Keep the three-route selector and product boundaries unchanged.
- Re-run canonical Claude/Codex delivery after merge before tagging a release.

## Release state — 2026-09-07

- beta.2 merged to `main` at `41793b5`;
- canonical GitHub marketplace is active in Claude Code and Codex;
- both installed caches execute PowerShell BOM task facts successfully;
- source/Codex/Claude selector SHA-256 parity is confirmed;
- release acceptance: PASS;
- release tag: `air-coder--v0.1.0-beta.2`.
