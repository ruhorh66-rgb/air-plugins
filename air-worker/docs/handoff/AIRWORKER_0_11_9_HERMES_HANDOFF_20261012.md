# AirWorker 0.11.9 — handoff для Hermes
**Дата:** 12.10.2026 (SGT). **Статус:** SOURCE-ONLY CHECKPOINT, `R0.11.9-02` OPEN / независимый судья NOT_RUN. **Owner передачи:** AirWorker → Hermes. **Trigger:** прямое решение ЛПР сохранить наработки, сделать scoped commit/push и передать продолжение Hermes. Это не решение о релизе/установке.

## 1. Неподвижные координаты

- **Узел:** `SRVLM01`; доступ к серверу и `AIR-ENV-002` — только AIR Commander с **явным node**. Сначала штатный старт AirOS по `F:\-0-\AIR_OS_START_HERE.md`, загрузка и проверка установленного плагина AirWorker; установленное ядро `air-worker 0.11.8`.
- **Каноническая нить:** `R:\-4-\air-worker\worktrees\airworker-0119-selflearn-isolated-20261011\air-worker\PLAN.md` и `plan/release-slices.json`; ветка `dev/airworker-0.11.9-selflearning-only-20261011`, проверенный `HEAD=bbfa050c4788b7c8d7960975825205e9e5cee3f3`, GitHub remote matching. В этом дереве **один чужой незакоммиченный** `air-worker/plan/N-071_rel-0-11-9-next-airworker-release-with-shared-le.md` с ошибочной старой целью `1.20`. Не добавлять, не чистить и не править `plan/N-*` в обход native core.
- **Рабочая ветка R02:** `dev/airworker-0119-real-capture-20261012` в `R:\-4-\air-worker\worktrees\airworker-0119-real-capture-20261012\air-worker`. **После получения передачи:** запросить actual HEAD/remote/dirty; этот документ и `docs/receipts/RELEASE_0_11_9_02.json` должны присутствовать в GitHub. Исходный source-only SHA эксперимента: `bbfa050c4788b7c8d7960975825205e9e5cee3f3`; код продукта не менялся.
- **Номер ближайшего релиза:** `0.11.9`, установленная/последняя опубликованная `0.11.8`; `1.20` — исправленная ошибка нумерации, `0.12.0` зарезервирован под отдельную архитектуру. Машинный sidecar содержит ещё восемь будущих patch-срезов `0.11.10`–`0.11.17`. Первый приоритет — только **доказанный сквозной самообучающийся цикл одного native CLI error-класса**.
- **Стандарт:** AIR_VIBECODING `2.0.0-rc.10`, DEV-135 (commit `7ccd0c9951760cd54650e0a4d910200116a33357`). Атомарный шаг 8–12 минут активной работы / цель дробления не более 15 минут; долгие QA/модель: один `job_id` и последующий readback. `plan/N-*` и LEARN **только через ядро**.

## 2. Что действительно выполнено

`R0.11.9-01` — фактический read-only аудит, машинная квитанция `docs/receipts/RELEASE_0_11_9_01.json` (commit `9df678ade45531d2a564512cb94582adc1e3bbf1`). В активном `release-slices.json` этот шаг `closed`; первый открытый — `R0.11.9-02`.

Для R02 по явному решению ЛПР выполнены **два последовательных Codex-исполнителя через штатный `air-worker executor dispatch`**, не через ручной запуск Codex:

1. `class=complex` → `gpt-6-astra:low`, `job_id=gpt__r0119-selflearn-20261012__1791769717287555800__9bdd2d9a`, `DONE`. Исполнитель не менял код; внутри model sandbox Go получил `Access denied` при temp/cache. Это не факт продуктового FAIL/PASS.
2. `class=bulk` → `gpt-6-luna:low`, `job_id=gpt__r0119-selflearn-20261012__1791770055249053700__f51596c1`, `DONE`. Составлен безопасный ignored runbook `.woody/r0119-02-host-capture-runbook.md` и проверены native пути. Оба job receipt сохранены в `.woody/jobs/`; не повторять dispatch.

На **самом SRVLM01 вне Codex sandbox** один targeted `go -C air-worker/cmd test . -run '^TestN110' -count=1 -timeout 180s` завершился `exit=0`, log SHA256 `075f86ac71d4e0928c65d0117f84de24c44a048395575d6f9ba079ca2df87b18`. Однократно собран source-only executable `R:\-4-\air-worker\work\0.11.9\real-capture-20261012\air-worker-0119-r02-source-only.exe`, SHA256 `cf16c2cd4b32a6c8ae3d3bc9504fb24cb3059e018b89884ff1e726b3faf3194c`. **Ничего не установлено.**

Один реальный нативный stimulus на **пустом одноразовом продукте** (`R:\-4-\air-worker\work\0.11.9\real-capture-20261012\scratch-product-r02`) с отдельно защищёнными `scratch-state-r02`/`scratch-runtime-r02`:

- `learn self enable -product <scratch-product> -runtime-root <scratch-runtime> -product-id air-worker` → `exit=0`; selector задавался **только** через `AIR_WORKER_HOOK_STATE_DIR=<scratch-state>`.
- `validate -product <scratch-product> -json` → **реальный `exit=2`**, исходного `run-config.json` нет. Никаких ручных LEARN events.
- Автоматически создана **одна** квитанция `cli-failures/AW-CLI-8443bb90365a2b1b3262a338.json` (SHA256 `0f0f62eb34b95de9cf237f268ea13df56f32b94524cafc969f826d23d16abc22`), native `action=validate`, `cause_code=validate-run-config-missing`, run `AW-CLI-8443bb90365a2b1b3262a338`; поля HMAC проверены на форму 64 hex, **не** считать это независимой криптографической аттестацией.
- В `scratch-runtime-r02/events.jsonl` ровно одно `run_completed` для того же RunID (event `EV-fba82d54f3a62eb56a6f0c49`), SHA журнала `0c74904d6c7f11614d6fc31db912c9b13d391e27d2d8e9526ebc8730988c6cc9`. `procedure_used=0`, и такое событие **нельзя выдумывать**.
- Нативный `review-batch` создал candidate `procedure` и ledger `applied`, целевой безопасный Markdown SHA `9e3bbf04419606742e076221d4b4a3ed08e98cb1f1f180dff7823cce8b80c095`. Важно: предварительный verdict — **`unjudged: judge not configured`**, proposal journal — `PENDING_LPR`. Не выдавать ledger `applied` за окончательно одобренное самообучение.
- Отдельная квитанция фактов: **`docs/receipts/RELEASE_0_11_9_02.json`**, схема `air-worker.release-step.real-native-capture/v1`, SHA256 `902559cbef78c81b72c76ab7016f82776e5d62bf163a9ac9a1501b61b9de6214`, статус **`SCOPED_NATIVE_CAPTURE_PASS_INDEPENDENT_JUDGE_PENDING`**. В ней полная корреляция RunID, событий и файлов.

**Внимание:** защищённые локальные `scratch-runtime-r02` и `scratch-state-r02` могут содержать ключи HMAC. Не переносить весь каталог в GitHub, публичный артефакт, Wiki или чат. В Git сохраняются только SHA/метаданные без ключей; оригиналы оставить на SRVLM01. Не повторять `validate` для того же stimuli с целью «получить PASS»: использовать исходный `r02-native-validate-once-attempt.json` и квитанцию.

## 3. Точное первое действие Hermes

1. Проверить `node=SRVLM01`, `air-worker version`, PLAN/release-slices, `air-worker plan spine`, `plan node list -open`, Git HEAD/remote/dirty и процессы. Сверить **уже сохранённый** R02 receipt; не запускать повторно Codex исполнителей, Go-тесты, `learn self enable` или `validate`.
2. Получить **независимый предметный** судейский результат **только через нативный `air-worker review code`**, `codex gpt-6-sol:low`, `read-only`, на чистом published SHA этой ветки. Исходная реализация единого reviewer сохранена в ветке `dev/airworker-native-sol-reviewer-20261011`, HEAD `6a1627816d971aed7a0560f2d038262efe6a8c08` (source-only временный бинарник, **не** установленная 0.11.8). Сформировать безопасное read-only задание в ignored `.woody/reviews/`; сначала `-preflight`, затем единственный запуск, далее только `job_id` readback. Judge должен отдельно оценить R02 capture, подписи/повторы и семантику `unjudged/PENDING_LPR/applied`.
3. Если Sol Low PASS именно **область R02**, отметить R02 `closed` в `plan/release-slices.json` после committed receipt/readback, не приписывая N-110/N-073 глобального PASS. Если судья находит P1, создать маленький source-only fixture/test шаг и вернуть RED→GREEN, затем новый exact SHA. **R03**: настроить реальный reviewer и подтвердить only safe nonblocking Markdown apply с rollback, без обхода LPR. **R04**: второй новый процесс загружает target@SHA, автоматически выбирает процедуру и пишет фактическое `procedure_used + outcome`. Только затем R05 full QA, R06 независимый судья/доказательства, R07 отдельный gate релизной публикации.
4. **Номер релиза `0.11.9`, не `1.20`.** Живая установка и UAC/reboot требуют отдельного явного «да» ЛПР; rollback на установленную 0.11.8. Никакой tag/release/marketplace promotion по этой передаче.

## 4. Остальные сохранённые работы, но НЕ блокеры первого релиза

| Предмет | Source-only checkpoint | Что НЕ завершено |
| --- | --- | --- |
| Core машинных release slices | `dev/airworker-plan-slices-core-20261012`, `d986e678181765e84a4477fab8b100aab28270dc` | Parser жёстко зависит от конкретных версий/количества; не подключён к plan-lint |
| N-124 распределённый PLAN | `dev/airworker-n124-native-identity-20261011`, `2e825982efd1b1aa468f073739b87a666bcb7b84` | A3-2 native close/outbox; N-124 OPEN |
| Native pinned Sol reviewer | `dev/airworker-native-sol-reviewer-20261011`, `6a1627816d971aed7a0560f2d038262efe6a8c08` | Установка и интеграция в live ядро НЕ выполнены |
| Shared learning library | `F:\-8-\air-modules`, есть чужая dirty WIP | `0.1.0` не released; в AirWorker ранее pinned immutable Go pseudo-version `v0.0.0-20261011024604-7e6968b40838` |
| N-071 старый широкий gate | OPEN и отдельный чужой незакоммиченный файл с `1.20` | Не закрывать/исправлять `plan/N-*` вне ядра; before release согласовать native scope |

## 5. Безопасное возобновление после обрыва

**Сначала readback — потом изменение.** Для любого шага проверить тот же `job_id`, `source_sha`, квитанцию, `git status`, PID/owner и remote SHA. `EXECUTION_UNKNOWN` означает **UNKNOWN**, не FAIL/PASS; второй запуск возможен лишь после подтверждения, что исходный mutable job не исполняется и не оставил результат. Сохранять scoped SHA/log, не проводить полную регрессию на каждом промежуточном WIP. N-узлы и LEARN менять исключительно через AirWorker core; чужое грязное дерево не трогать.

**Приёмочный критерий этой передачи:** GitHub содержит оба handoff-файла и R02-квитанцию на одной опубликованной dev-ветке; SHA remote == local; рабочая ветка чистая, а каноническая N-071 остаётся нетронутой. Передача исходников ≠ PASS судьи, релиз или установка.
