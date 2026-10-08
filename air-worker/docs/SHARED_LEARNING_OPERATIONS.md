# Общий модуль самообучения — кандидат разработки 05.10.2026

Статус: P0 shared-learning реализован в кандидате 0.11.7 и прошёл изолированный цикл `init-shared → finalize/review → auto-apply procedure → next-run load exact SHA → outcome/rule_id`; production ещё не переключён. Исходный live AirWorker 0.11.5 остаётся установленным до публикации проверенного 0.11.7. Квитанция функционального цикла: `docs/receipts/LEARNING_P0_ISOLATED_ACCEPTANCE_20261006.json`. Publication остаётся отдельным exact-SHA гейтом; после публикации ЛПР 06.10.2026 явно разрешил установку и обновление Claude/Codex cache. Reboot/UAC/остановка чужих служб/ASW/bridge этим не разрешены.

## Один владелец состояния

`learning-module.json` в корне продукта выбирает общий Go-модуль для этого продукта. Без файла прежний установленный контракт не переключается. При наличии файла неподдерживаемая команда возвращает явную ошибку; запись в прежний LEARN не служит запасным вариантом. Активные старые ограничения требуют отдельного согласования перехода, а не молчаливого отключения. Исторические журналы не импортируются автоматически.

Конфигурация схемы `air-worker.shared-learning/v1` явно задаёт `product_id`, абсолютный `runtime_root` (в AIR — на R:), `managed_skill_prefix` (например, `skills/learned`) и защищённые `protected_targets`. Один физический GitRoot имеет одного владельца RuntimeRoot: локальный файл адресации `.air-learning-owner.json` не включается в Git. Это адресация, не журнал; события, review, ledger, backup и диагностические следы остаются в RuntimeRoot. Смена владельца — отдельный контролируемый переход.

### Собственное самообучение AirWorker — кандидат 0.11.9

`air-worker learn self enable -product <AirWorker-GitRoot> -runtime-root <absolute-R-path>` включает **тот же shared-learning engine**, но публикует отдельный host-level selector `air-worker.self-learning/v1` в состоянии AirWorker. Поэтому собственный learning AirWorker остаётся доступен, когда активная сессия разрабатывает Vera, AirWiki, AirCurator или другой продукт без собственного `learning-module.json`. `learn self status` проверяет ProductRoot/ProductID/RuntimeRoot и exact SHA конфигурации; повреждение или незаявленная смена конфигурации дают ошибку, а не молчаливое отключение. `learn self disable` и `learn self rollback` выключают только host selector и не удаляют Git-процедуры или runtime-журнал.

SessionStart/UserPromptSubmit доставляют self-context параллельно product-context; одинаковый физический root не загружается дважды. Stop записывает завершённый run обоим владельцам, если они различны. Для self-owner host хранит отдельную per-session/run квитанцию точных `target@SHA`, реально переданных в контекст. `loaded` не считается `used`: применение записывается только после последующего машинно наблюдаемого результата. Первый реализованный evidence-adapter для `execution-unknown-no-blind-retry` принимает `EXECUTION_UNKNOWN` как trigger, отвергает слепой повтор `start_process` как доказательство и записывает `procedure_used`/`outcome=pass` только после успешного диагностического readback с тем же exact `target@SHA`.

## Вызовы и границы

- `air-worker learn init-shared -product <root> -runtime-root <absolute-R-path> [-product-id <id>]` — транзакционно выбирает shared writer и мигрирует только уже APPLIED `operational-procedure-v1` в `skills/learned`. Исходные Git-tracked rule records остаются byte-identical reproducibility seeds; shared mode делает legacy state read-only и проверяет exact overlap с bootstrap intent. Machine-local `learn/legacy-rules/` хранит provenance и не входит в Git. Если релиз уже содержит exact managed procedure bytes, новый пустой RuntimeRoot не доверяет им сам по себе, а повторно строит ownership/ledger через shared module. Любой иной или изменённый legacy rule блокирует переход.
- `air-worker learn status -product <root>` / `paths` — версия модуля, фактические пути и последний след.
- `air-worker learn event -product <root> -run-id <id> -class <class> -observed <text> -actor <actor>` — наблюдение и автоматический review завершённого прогона.
- `air-worker learn finalize -product <root> -run-id <id> -observed <text> -actor <actor>` — штатное завершение внешнего прогона. Для GPT обязателен стабильный run_id; транскрипт Claude не требуется.
- `air-worker feedback add -product <root> -kind error|idea|lesson -text <text> -ref <receipt>` / `feedback list ... -json` — тот же вход и тот же журнал.
- `air-worker learn context -product <root>` — проверенный индекс; `learn load -product <root> -target <relative.md> -run-id <id>` — точные байты и SHA с квитанцией загрузки.
- `learn propose` с `-kind procedure`, `-target`, `-pre-sha256` и `-content-file` применяет допустимую процедуру автоматически. Новый файл требует пустого pre-SHA; существующий — точного SHA. Текст должен иметь заголовок и разделы Procedure/Pitfalls. Запись допускается только в делегированную область, но чтение стартовых навыков само по себе не делегирует запись.
- `learn pending`, `diff`, `approve`, `apply`, `rollback`, `effect`, `summary` используют общий протокол. Сложный JSON передаётся через `-data-file` или `learn module` stdin; конфигурацию потребителя stdin не переопределяет. Готовые JSON-примеры — в тестах `cmd/learn_shared_test.go` и контрактных тестах общей библиотеки.

JSON и код возврата проверяются вместе: 0 — операция исполнена, 2 — ошибка, 3 — конфликт SHA/ID или недоверенное разрешение. `recorded` означает записанное событие, а не успешное обучение: в данных отдельно стоят `review_outcome`, ID и RV. `loaded` и `used` не синонимы. Загрузка байтов не доказывает влияние на работу.

## Реальные адаптеры и проверка поставки

Для AirWorker 0.11.7 Reviewer — release-owned `@self` adapter: тот же exact binary запускает `learning-adapter reviewer`, передаёт product root через защищённый process env и вызывает существующий, уже принятый learning Codex/Ponytail read-only route (`gpt-5.6-luna`, medium). Reviewer может вернуть только procedure-кандидат в managed subtree либо пустой результат; grants, executable rules, permissions и policy он не создаёт. Общий review ограничен 300 s, process-adapter — 240 s. На Windows deadline завершает всё дерево запущенного adapter/Codex процесса; дочерние `cmd/node/codex` не оставляются фоном. Внешние Judge/VerifyGrant/DeliverSummary при их настройке остаются hash-pinned процессными адаптерами с JSON stdin/stdout, deadline и bounded output. VerifyGrant обязан проверять истинный канал ЛПР и те же proposal_id/diff_sha256/decision/channel_ref; без него executable `check_spec` не получает approval. Доставка требует message_id/channel_ref и readback SHA фактически доставленного сообщения.

Отсутствие адаптера видно как unjudged, RV error или pending_delivery. Локальный файл не считается доставкой человеку. Go-callback обязан соблюдать context; процессный адаптер ограничивает и при deadline завершает своё дерево процессов. Реальный reviewer provider проверен отдельным exact-source smoke; маршрутизация доверенного разрешения для executable `check_spec` и фактическая доставка summary остаются отдельными ограниченными функциями.

## Что доказано и что пока открыто

Unit/subprocess тесты покрывают создание/изменение/загрузку процедур, проверку SHA, откат, восстановление оборванных транзакций, защиту идентичности, конфликтующие процессы, ограничения путей, повторные разрешения, ошибку/таймаут reviewer и process exchange. Дополнительно на SRVLM01 выполнен изолированный реальный цикл: три legacy operational procedures мигрированы через `init-shared`; `finalize` вызвал release-owned `@self` Codex reviewer и автоматически применил новый procedure; следующий run загрузил exact target/SHA; отдельная машинная квитанция PASS и `finalize` связали outcome с `rule_id`; повторный review вернул `no_candidate`. Это не production cutover и не семидневный effect.

Открыты перед эксплуатацией: exact-artifact full regression + независимый semantic PASS, GitHub publication по отдельному exact-SHA гейту, затем уже разрешённые ЛПР install/cache refresh и live installed-binary smoke. VerifyGrant/live block для executable `check_spec`, реальная доставка daily summary и семидневная статистика effect остаются отдельными ограниченными функциями и не блокируют процедурное самообучение.

## Восстановление

При конфликте SHA не повторять запись вслепую. Прочитать `learn status`, фактический файл и ledger; сохранить стороннюю правку. Незавершённая транзакция восстанавливается при следующей операции под блокировкой. Не удалять owner/WAL/grant-файлы вручную, чтобы обойти отказ. Ошибка канала или модели не превращается в пустой успешный review. Старый журнал при переключении не переписывается.

### Повтор закрытия узла после частичного сбоя

Для shared-режима ядро сохраняет намерение закрытия в `runtime_root/plan-close/<SHA>.json` до изменения узла. Это outbox транзакции плана, а не второй LEARN: события по-прежнему записывает только общий модуль. Перед созданием намерения проверяются владелец состояния и доступность модуля.

Сбой после сохранения события не откатывает узел вслепую. Команда возвращает ненулевой код и `action=pending` с `recovery_ref`; повтор той же команды с исходной квитанцией продолжает ту же операцию. Исходные время, actor, receipt и идентичность события не меняются, даже из новой сессии. Иная квитанция или сторонняя правка узла дают конфликт и сохраняются без перезаписи. Восстановление обновляет только соответствующую ссылку текущего PLAN, а не старую копию всего плана. Завершённый повтор также возвращает JSON. Эти проверки воспроизводят прерывание записи и перезапуск команды, но не заменяют испытание отключения питания.

Для `learn event/add/finalize -data-file` используются поля JSON, а не значения отсутствующих флагов. Только явно заданный флаг, в том числе пустой, переопределяет соответствующее поле; неподходящий тип `run_id` отвергается до записи. Повтор завершённого run_id возвращает конфликт/duplicate с кодом 3, а не создаёт новый прогон.

### Создание и миграция узлов в shared-режиме

`plan node new` и `plan migrate` требуют явный `-request-id <стабильный ID запроса>`, поскольку новый узел ещё не имеет идентичности. Прежний режим без `learning-module.json` совместим с прежними командами. Повтор того же запроса с тем же ID возобновляет outbox `runtime_root/plan-create`; другой запрос с занятым ID отвергается до изменения данных. Новое намеренное создание с тем же заголовком получает новый request-id.

Намерение содержит исходные время, actor и точные снимки узлов. При частичной записи пакета миграции повтор досылает недоставленные события с прежними ID. Ошибка записи следа после сохранения события не удаляет созданные узлы и не восстанавливает старую копию PLAN. Отдельные процессы потребителя проверены для всех трёх операций: new, migrate и close. Создание/миграция — наблюдения, не завершённые прогоны: они не запускают фиктивный review. При закрытии Node, Body и BeforeSHA разбираются из одних байтов; правка во время preflight вызывает отказ с сохранением правки.
