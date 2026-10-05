# AirWorker — передача остановленной сессии

Дата: 2026-10-05T21:32:58.137295+08:00 SGT. Узел: SRVLM01. Статус: **SAVED_WIP / NOT_RELEASE_READY**.

Последнее поручение ЛПР: завершить сессию, сохранить наработки и сделать коммит. Новая разработка, тесты, выпуск и установка при сохранении не запускались. Это handoff, не второй план и не приёмка реализации.

## Единственная нить работы

PLAN: `F:\-7-\_worktrees\airworker-learning-20261005\air-worker\PLAN.md` и его `plan/N-*`. Ветка: `dev/airworker-learning-20261005`. База: `b53163f0c9332efd8a2c2c69dcc889237dc0bd6a`. Исходное старое dirty-дерево `F:/-7-/air-worker` не менять, не выполнять reset/clean.

Ближайший незакрытый узел: **N-040_learn-fix-round4-pending-transaction-gate-flushe**. Начать продолжение с его done_when и четвёртого вердикта Astra, не с повторного проектирования. Узлы N-029..N-040 при сохранении не закрывались. Исторические PASS не заменяют новую живую приёмку.

## Сохранённый код и зависимость

Сохранены shared-learning facade, JSON/feedback/hooks, подготовка и восстановление plan new/migrate/close, отрицательные тесты, плановые узлы и штатный журнал. Это промежуточная редакция с известными дефектами, не готовый релиз.

Модуль: `F:\-8-\_worktrees\air-modules-airworker-20261005`; checkpoint `dd5b6efad6c4d319f78e910d61cc6be8ddf2d8f8`. В AirWorker по-прежнему закреплён `v0.0.0-20261005044632-2ca7e731607e`. Более поздняя правка теста модуля не означает смены pin потребителя.

Четвёртый цикл исправлений успел сохранить узел, входные снимки и документацию. Фактическое совпадение текущих файлов с подготовительными SHA находится в `round4-source-comparison.json` резервной копии; наличие N-040 само по себе не означает, что его исправления реализованы.

## Последний независимый судья

`R:\-4-\air-worker\work\learning-20261005\airworker-review-4.verdict.json`: **CHANGES_REQUIRED**. Назначенный судья — Codex Astra `gpt-6-astra`, read-only.

- P1: `cmd/plan_shared_create.go:115` — New requests ignore other prepared intents. After a two-node migration persists only its first event, repairing the trace obstruction and running another new/close command succeeds. Retrying migration then rejects the changed PLAN/node before delivering its remaining event. A crash before node publication also leaves an ID reservation invisible to nextPlanNodeID. The tests restart the same request without interleaving another mutation.
  Required fix: Under the product plan lock, reconcile or reject conflicting prepared create/migrate/close intents before permitting another mutation or allocating IDs. Add subprocess checks that interleave new and close with a pending migration.
- P1: `cmd/plan_shared_close.go:45` — Both intent persistence functions use writeFileAtomic, whose Windows implementation writes, closes and renames without Sync; the other implementation also lacks Sync. Consequently the purported durable intent can be lost or corrupted during an OS crash while a flushed learning event survives. Recovery then loses the original identity or mistakes a closed node for a historical closure. Fresh-process retry tests do not exercise this durability gap.
  Required fix: Persist intents through a flushed atomic writer before publishing nodes, PLAN changes or events, and propagate flush failures. Apply this to both close and create/migrate intents, with the required platform publication guarantees.
- P2: `cmd/learn_shared.go:202` — Adapter stdout and stderr are bounded, but stdin is not: arbitrary-length callback input goes directly into bytes.NewReader. The pinned summary implementation aggregates all procedure changes and pending blocks, so normal journal growth can send an arbitrarily large JSON payload to the delivery process. The CLI request limit does not bound this generated payload.
  Required fix: Reject oversized adapter input before launching the executable, using an explicit byte limit. Add a check proving oversized input never starts the adapter and summary delivery remains pending with an explicit error.

## Проверки и пределы доказательства

Точные прежние команды, коды возврата и время сохранены в `docs/receipts/SESSION_SAVE_20261005.json`. Новая полная регрессия этой сохранённой редакции при завершении не выполнялась. Последний успешный тест не подменяет PASS судьи; незавершённый или timed-out factual judge не считается PASS.

Реальный напарник и канал сводки, полный host capture, наблюдаемое применение процедуры следующим обычным запуском, доверенное live-разрешение блокировки и её action enforcement всё ещё требуют отдельной приёмки. Fixtures и статический review не выдаются за живое обучение.

Проверенная установленная версия: `air-worker 0.11.5`. В ходе сохранения установленный продукт не менялся.

## Разрешения и остановка

N-037 хранит разрешение ЛПР на публикацию после проверок. Текущая команда остановила выполнение этой сессии: сейчас выпуск не производится. Установка, production cutover, конкретный live block, reboot/UAC и остановка чужих служб не разрешены этой командой.

ЛПР сообщил о повторных зависаниях ответов; причина не установлена. Сохранить квитанции и продолжать в новом окне короткими измеримыми шагами. Не считать отсутствие ответа доказательством отсутствия изменений: сначала readback плана, Git и собственных процессов.

## Восстановление контекста

Прочитать этот handoff и PRODUCT_INSTRUCTION, затем штатные `plan spine` и `plan node list` с указанным product. Продолжать N-040; не редактировать plan/N-* и LEARN в обход ядра. После исправлений нужны свежие полные тесты, factual judge без таймаута и независимый Astra на точный manifest; закрытие только по факту и квитанции.

SHA-проверенная резервная копия исходных WIP-файлов: `R:\-4-\air-worker\work\learning-20261005\session-save-20261005-213258`. Все исходные логи, prompts, raw model events, рабочие скрипты и тестовые бинарники остаются в `R:\-4-\air-worker\work\learning-20261005`; они не удалены и не добавляются массово в Git.
