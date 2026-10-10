# AirWorker 0.11.9 — единый WIP handoff

**Дата:** 11.10.2026 SGT. **Сервер:** SRVLM01 (Франкфурт). **Владелец:** AirWorker developer.
**Ветка с полным WIP:** `wip/airworker-complete-handoff-20261011` в worktree `F:\-7-\_worktrees\airworker-curator-skill-lifecycle-20261006`.
**Базовый SHA:** `4f9ca27d56bf2822ee859f10d931f75005ada673`. Коммит хэндоффа — HEAD этой ветки после push.
**Безопасная ветка разработки до WIP:** `dev/airworker-curator-skill-lifecycle-20261006` остаётся на исходном SHA.
**Живая версия:** 0.11.8, НЕ обновлялась. **Этот WIP не является релизом.**

## Что включено в checkpoint

Работа собственного самообучения и плана: `self_cli_failure.go` + тесты (нативное наблюдение ненулевых CLI-кодов в own LEARN с отдельной квитанцией, без аргументов/секретов, отказ при конфликте run_id), `learn_shared_deferred_recovery.go` (recovery deferred review), `plan_parent_integrity.go` и `plan_nodes_reparent.go` + тесты (отказ на неверном родителе, штатное reparent с атомарным readback, журналом, повтором по request-id), `main.go`, `plan_nodes.go` и узлы N-110–N-112. Узел N-111 уже был исправлен реальным **нативным** `plan node reparent`; квитанция и журнал — `R:\-4-\air-worker\work\0.11.9\n112`, `R:\-4-\air-worker\self-learning\plan-reparent`. Журналы LEARN и файлы узлов **не правились напрямую**.

Другие накопленные изменения: N-113 (Windows-пути профилей), `job_receipt`, `tool`, `ladder`, `model_policy`, `semantic`, `semantic_hermes_test`, `tool_router_test`, `run-config.json`, learned skill `skills/learned/plan-node-9c553a82-a00094a9959bb009.md`, N-114 и N-115. Все эти изменения сохранены как WIP с полным состоянием, без частичной потери при смене окон.

**Важный release-блокер:** `run-config.json` в WIP меняет ступени для Claude на `kind=hermes`, `provider=nous` и модели Nous Portal. **Решение ЛПР — отложить Nous Portal/OmniRoute на релиз ПОСЛЕ 0.11.9, потребовать дополнительное согласование.** Поэтому этот WIP нельзя напрямую включать в 0.11.9, пока Hermes/Nous изменения не будут изолированы, проверены и перенесены в отдельную будущую ветку. Узел N-115 отмечен CLOSED в историческом плане, однако код/конфигурация НЕ означают разрешение ЛПР на установку, включение провайдера или публикацию. Нужна новая нативная корректировка релизного spine при продолжении.

## Реальные проверки и пределы доказательств

- `go test ./... -run 'TestN110|TestN112|TestN109|TestN108|TestN104|TestN105|TestN096|TestN102|TestN095|TestAirWorkerSelfLearningExecutionUnknownUseNeedsCorrelatedDiagnosticOutcome|TestHermes|TestLadder' -count=1` — оба пакета **PASS**, код 0.
- `go vet ./...` — **PASS**, код 0.
- `air-worker validate` — 7 целей / 62 критерия / 0 проблем; `plan-lint` — 0 предупреждений. Это не является global `judge PASS`.
- Нативная операция восстановления N-111 по точным node+PLAN SHA и идемпотентный повтор — PASS в изолированном бинарнике N-112.
- **Полная Go-регрессия на новом WIP-коммите НЕ ПОДТВЕРЖДЕНА**. **Независимый GPT-6 Sol / Low / Medium на этом SHA НЕ ЗАПУСКАЛСЯ**. Реальный P0 N-110 review→auto-apply→future-use→effect для новой ошибки вне unit-теста **НЕ ДОКАЗАН**.
- Перед handoff живой AIR Commander 0.11.8 не переустанавливался; тег/релиз/службы/UAC/reboot не трогались.

## Первая последовательность следующего окна

1. На SRVLM01 через AIR Commander с явным node прочитать HEAD/dirty, PLAN.md и N-070/071/073/092/093/108/109/110/111/112/113/114/115, проверить native LEARN receipts. Загружать последнюю штатную версию AirWorker plugin из GitHub только разрешённым способом, проверять live version.
2. **Разделить** WIP Hermes/Nous и 0.11.9: сделать независимую ветку для следующего релиза; в 0.11.9 оставить безопасные уже согласованные self-learning/plan улучшения. Не трогать чужие приложения и реестр.
3. Добиться P0 N-110: настоящая own CLI/error auto capture → trusted reviewer → safe auto-apply → loaded and used in next unrelated session with verified effect; никакого false positive от произвольного текста или ручного `target@SHA`.
4. Проверить N-108/N-109 независимым GPT-6 Sol с указанным ЛПР усилием (точный доступный model ID) и один full `go test ./...` / vet на чистом HEAD без параллельного тяжёлого reviewer; N-092 global judge отдельно.
5. AirVera N-059 зависит от **другой** ветки модуля: `https://github.com/ruhorh66-rgb/air-modules`, `dev/air-learning-v0.1.0-airvera-20261010`. Точный WIP-коммит и отдельный хэндофф: `docs/receipts/AIRLEARNING_N027_WIP_HANDOFF_20261011.md`. Не закрывать N-059, пока нет опубликованного тега модуля и подтверждённой записи владельца центрального реестра.
6. Коммит и push отдельных продуктов, судья PASS, машинные квитанции. Публикация GitHub Release/tag и установка только после отдельного явного «да» ЛПР. Откат при будущем разрешённом релизе — к установленному бинарнику 0.11.8 с проверкой SHA и сохранением LEARN.

**Состояние:** `WIP_PRESERVED_NOT_RELEASE_READY`. Подготовка хэндоффа не закрывает открытые N-* и не заменяет судью.
