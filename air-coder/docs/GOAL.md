# Goal — AirCoder 0.1.0-beta.1

plan_for_version: 0.1.0-beta.1
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
