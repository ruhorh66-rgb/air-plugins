# AirWorker · Dev Work 30 → следующая GPT-сессия · 08.11.2026

## Продолжение Dev Work 31 · 08.11.2026 · микрошаги работают в исходниках, три QA-отказа устранены адресно

Сохранив текущий единственный PLAN и прежние receipts, Dev Work 31 (ChatGPT координатор) поставил **два малых разных кодовых шага двум реальным Codex**, каждый в собственном Git-worktree и на своём файле. Новый исходный код `cmd/orchestrate_coding_workers.go` требует строгий `air-worker.coding-microsteps/v1`: ровно два разных `file/title/instructions/done_when`, fail-closed до моделей при пересечении/небезопасном/неполном задании, каждому передаёт только свою инструкцию, после выполнения сверяет фактический Git diff с его file scope; `cmd/orchestrate_coding_workers_test.go` добавил и прошёл негативные тесты. Первый цикл на старом source-only launcher исходного SHA `941e21a`: 2×Codex real edits, `DONE/RC0`, 2 patch SHA и `git apply --check` PASS, адресные `TestCoding|TestChatGPTCoordinator|TestTwoWorker` PASS, `go vet RC0`. Receipt `docs/receipts/DEVWORK31_TWO_CODEX_MICROSTEPS_SOURCE_20261108.json`; source commit `6cf397479027d9390096f8ff64521b27cdd77a82`. Текущий полный source SHA уточнять по live Git, следующий checkpoint может быть позже.

**Живой readback новой маршрутизации:** из чистого `2371033df17d35fc127c3a76472f9742d4c1f243` source-only CLI SHA `5a84185cc0128a1f4026eb85ed838c4e02b65c72f5900a6f617bfddcf08120f3` создала два отдельных `TASK-CODE-WORKER.md`; worker 1 получил только `cmd/criteria.go`, worker 2 — только `cmd/native_route_contract_test.go`; другая инструкция отсутствует, оба microstep SHA собственные, исходный Git до merge CLEAN. Оба Codex `DONE/exit=0`, patch SHA `ef427710978355413936f1d6602ec4e5998893ae02dbc0e00749391a9ee0c5b1` и `65bd9c6f124f588418089411151942a6a7fa772d631332a0e08d97aa14992794`; `git apply --check` RC0 2/2; координатор интегрировал. Третьего лидера не было. Hermes skill компактно переписан без модели, 2517→1445 байт, SHA `7436fe4b9e434876732e4ba11a706d10b04029d91499db5669fd5569acdb24e6`. Адресные ранее отказавшие Go-тесты **3/3 PASS RC0**: PowerShell selector, Hermes contract и native N129 startup; `go vet RC0`, log SHA `43ea84c47f4e9fa63b7366063dd5b593124b9bc175a85c3739b615c2243949a5`. Receipt `docs/receipts/DEVWORK31_P0_QA_TWO_MICROSTEPS_20261108.json`.

**Границы:** это не полный N123 QA, G05/G06/G07/G08 не закрыты, P0 остаётся 4/8 и 0/4 native CLOSED. Полный Go ранее `RC1`: кроме трёх ныне адресно исправленных отказов остаётся N124 `TestN124PlanRephaseJournalSnapshotLinkedFileHold/node.before` timeout; повторять большой прогон без адресного исправления не надо. Реальное G07 требует двух межчатовых разрывов + `EXECUTION_UNKNOWN/zero duplicates`; G06 — реального причинного provider failure/stall; G05 — полный финальный QA, независимые Sonnet + Codex Sol low, rollback и source SHA. Текущая установленная версия `0.11.11-rc.2` неизменна, candidate `rc.5` SOURCE_ONLY/HOLD; старые worktrees/receipts и чужой R:-WIP не чистить. Следующее исполнимое — один маленький самостоятельный N124 диагностический шаг, затем source final-SHA приёмка.


## Дополнение Dev Work 31 · 08.11.2026 · G05C integration и последующая приёмка

У новой беседы свой отдельный ChatLink session_ref; не наследовать Work 30. Точный идентификатор передан Vega приватным транспортом и не публикуется в Git. Загружено из двух уже созданных code-writing worktree `e446540ac0ff9c1628`, исходный baseline `028bff3`; два job `gpt__dw30g05c__1794114152126054100__d473bfaf` и `gpt__dw30g05c__1794114152126719400__19771374` DONE/RC0, патчи SHA `cf6864c6758c1272c513195ad392c051c8c09868a05ad5c0ec6e7c95926bbd18` и `89b50cc45bd910029b4aa287bece299eff252153a028148a30fb9c0c3468fef2`. Оба `git apply --check` RC0 на исходном `2f6aa1159`. ChatGPT-координатор интегрировал `cmd/source_review.go` и `cmd/source_review_test.go`, `go test . -run TestSourceReview -count=1 -v -p 2`/ `go vet -p 2 .`/ `git diff --check` RC0. **Symlink security case SKIP, не PASS**, Windows privilege absent. Source receipt `docs/receipts/DEVWORK31_G05C_SALVAGE_20261108.json`; final committed SHA всегда читать из Git, а не из старой записи.

Это подтверждает G05C **адресную QA**, но не закрывает G05 и не отменяет G07→G06→G05→G08. P0 всё ещё 4/8, четыре native nodes OPEN. G07 нужна пара настоящих отдельных GPT-chat disconnect и EXECUTION_UNKNOWN/zero replay; G06 нужен реальный failure/stall вместо synthetic vendor_limit; для G05 — полный N123 QA + independent Anthropic и Codex Sol low + rollback на clean SHA. G01–G04 не повторять.

ChatLink HELLO к Vega поставлен в очередь со своим новым session_ref; подтверждение ответа отдельно. Временные идентификаторы не включать в публичный Git. URL беседы недоступен в имеющемся connector, не угадывать. Рабочие G05C копии и квитанции оставить целыми. Shared-learning процедуру `LP-2dcd5e164b352092b65fbf08` не дублировать, G05C является реальным применением процедуры, но причинное предотвращение ошибки в новой сессии не доказано. `rc.5` SOURCE_ONLY/HOLD, installed `rc.2` unchanged.


## 0. Краткий статус и граница

**Решение ЛПР:** сохранить сделанное, обновить **существующие** планы по фактам, оставить оба собственных Git-дерева чистыми, обучить действующую базу самообучения AirWorker результатам кодинга, закрепить последнюю схему оркестрации, продолжить P0 уже в новой сессии.

**P0: 4/8 PASS, 4 OPEN (G07, G06, G05, G08). N123/N126/N136/N139: 0/4 CLOSED.** Нет оснований считать P0 завершённым. G01–G04 приняты из прежних квитанций — не тратить модели на повтор. Самообучение и его регистрация в этом хендоффе разрешены ЛПР отдельно, но это НЕ автоматический PASS P1/effect.

**Git/source:** repo `D:\Devbox\AirWorker\worktrees\awlrn-orch-pilot-20261104` / `ruhorh66-rgb/air-plugins`, branch `dev/airworker-native-orch-pilot-20261104`, product `...\air-worker`. **Source baseline до подготовки handoff:** `028bff3e923dcbf340a663242829793b3a4e8250`. Финальный HEAD хендофф-коммита и точный upstream проверить в новой сессии `git rev-parse HEAD` / `git rev-parse '@{upstream}'` / `git status --porcelain=v1`: никакой сохранённый в этом документе старый SHA не отменяет live readback.

**Wiki:** `F:\-5-\011_Plugins\AirWorker_Wiki`, branch `dev/airworker-curator-0.10.14`, канонический `00_STRATEGY/AIRWORKER_PRODUCTION_PLAN.md` и машинный `.json`. Все сохранённые исторические блоки ниже новых checkpoint — история; актуальны верхние разделы.

**Установлено сейчас:** AirWorker `0.11.11-rc.2` (не обновлялся), Codex CLI `0.167.0` (ChatGPT login), Claude Code `2.1.319`. Source-candidate `0.11.11-rc.5` НЕ выпущен, НЕ установлен. Старый distribution B `a96e2a2c107765be43a1bec6263b1d37f892d2c2` связан со старым source SHA и не может подписываться для свежего кода. Предыдущий GitHub `rc.4` не менять.

## 1. Единственный актуальный режим оркестрации

**ChatGPT этой новой обычной беседы = координатор/оркестратор. Ровно ДВА Codex — самостоятельные кодирующие воркеры.** Не считать двумя кодерами два read-only LLM-обзора, не заводить третьего Codex-лидера и не заменять параллельный режим двумя последовательными `executor dispatch`.

Штатный **source-only** вход кандидата `rc.5`:

```powershell
air-worker session declare -product "<product-root>" -principal gpt -session-key "<own-new-session-key>"
air-worker orchestrate code -product "<product-root>" -task-file "<bounded-TASK-inside-product>" -principal gpt -session-key "<own-new-session-key>" -tier gpt61-terra:low -json
air-worker orchestrate code status -product "<product-root>" -task-file "<same-TASK>" -principal gpt -session-key "<original-owned-session-key>" -json
```

`orchestrate code status` — READ-ONLY для существующего scope, не означает право rerun в новом чате. Каждому Codex `workspace-write` разрешён **только в собственном выделенном Git worktree от одного clean HEAD**. Модель физически меняет назначенный исходник и выдаёт отдельный terminal receipt; AirWorker проверяет `git diff HEAD` и SHA. Координатор лично проверяет 2 changed-file lists, `job_id/status/exit_code/args/source_head`, `git apply --check`, последовательно интегрирует в единственный основной worktree, запускает Go factual тесты и отдельного Anthropic судью на exact SHA. `READY_FOR_CHATGPT_COORDINATOR_INTEGRATION` = только готовность принять изменения, не closure. Не использовать прямые `codex exec`/Claude provider API и PAYG.

**Эффективная модельная лестница source:** `script → GPT-6 Luna → GPT-6.1 Terra → GPT-6 Sol → GPT-6.1 Sol → GPT-6 Astra`, low/medium/high; повышение только после реальной причины на том же TASK и исходном SHA. Судьи Anthropic: Sonnet 5.5 основной, Opus 5.5 complex, Fable 5.1 arbitration, Haiku 5.5 только triage. `--json-schema` у источников Anthropic добавлен в текущем source; до нового независимого заключения нет финального G05 PASS.

**Исторические факты coding:**
- `docs/receipts/DEVWORK30_REAL_AIRWORKER_CHATGPT_TWO_CODERS_20261108.json` — фактические изменения двумя GPT-6.1 Terra Codex воркерами в разных файлах, ChatGPT координировал интеграцию, целевые Go/vet PASS, без третьей модели.
- `docs/receipts/DEVWORK30_P0_G07_RECOVERY_SOURCE_ONLY_20261108.json` — два настоящих Codex-кодера source+tests для recovery, read-only status/unit PASS. **0/2 настоящих GPT-chat disconnect**; G07 OPEN.
- `docs/receipts/DEVWORK30_P0_G06_CAUSAL_SOURCE_ONLY_20261108.json` — два кодера и bounded causal source, но vendor_limit только **синтетический**; G06 OPEN.
- `docs/receipts/DEVWORK30_P0_G05_STRUCTURED_CLAUDE_SOURCE_ONLY_20261108.json` — новая строго структурированная независимая Anthropic source-review реализация, негативные тесты, но G05 OPEN.
- Последние source commits после этих срезов `9884dc0` (`--json-schema`), `92af990` (строгие пути цитат), `028bff3` (Windows-safe TASK identity). **Не переносить PASS с предыдущих SHA на финальный без нового judge.**

## 2. Реальное P0 / кратчайший путь

| Gate | Уже принято | Осталось закрыть по exact done_when |
|---|---|---|
| G01–G04 | Исторические machine/CodeX/Go/Anthropic PASS | Не запускать повторно |
| G07 / N123 | `orchestrate code status`, read-only immutable readback, RUNNING/UNKNOWN отрицательные tests | Два реальных разрыва **обычных GPT-сессий** при одном нативном job; recover `EXECUTION_UNKNOWN` с 0 дубликатов, no foreign kill |
| G06 / N136/N126 | На same TASK/SHA native `orchestrate code escalate` в synthetic vendor_limit fixture прошёл | Один реальный причинный provider failure/stall → следующая допустимая модель, 2 native worker jobs, immutable attempt receipts |
| G05 / N123+N139 | Source-only `source_review.go` на `--json-schema`, strict validator; targeted Go/vet PASS на этапах | Зафиксировать **последний** SHA; один required N123 full QA, отдельный Anthropic source/security `TASK_READ_OK PASS` и независимый Codex Sol low PASS, reversible rollback, remote Git SHA |
| G08 / N123/N126/N136/N139 | Узлы существуют и OPEN | Нативно `plan node close` только после полного собственного done_when, не редактировать `plan/N` и accepted SHA вручную |

**Важное:** `tools/p0-progress.ps1` на наблюдаемом исходном `028bff3` показывает 4/8 и предупреждение: `operational_goal_source_sha=47004e0` не совпадает с HEAD. После финального handoff-коммита **один раз** выполнить штатный `operational-goal advance -from-head 47004e0... -source-head <новый чистый HEAD>` с собственным корректным session ID, затем `status` и `p0-progress.ps1`; не менять release-slices SHA `811a37794842d8ed575f478d0a84665579daee5f5afe94e5e4ad6e9e7627ab9a` (сверять также с native status). Если `from-head` уже изменился в другой сессии, readback и отказ от слепого повторения.

## 3. Собственное обучение AirWorker — сохранено

Host `air-worker learn self status -json` показывает **enabled**; authoritative selector owner — `R:\-4-\air-worker\worktrees\airworker-0119-n093-worker-b-20261018\air-worker`, runtime — `R:\-4-\air-worker\work\AIRWORKER_0119_R02_PRIMARY_20261020\runtime`. Этот старый owner имеет **чужой dirty WIP**, его не чистить, не reset, не transfer-owner без отдельной причины. Native shared-learning процедура уже обучена, не создавать дубль:

- `air-worker learn event -product <owner> ...` записал `EV-be7252413d0b8e4d91caf165`, RC0, status `recorded`, auto review `error` — не выдавать за reviewer PASS.
- `learn propose -kind procedure -target ... -content-file docs/learning/DEVWORK30_TWO_CODEX_COORDINATOR_PROCEDURE_20261108.md` создал `LP-2dcd5e164b352092b65fbf08`, status **APPLIED**, native transaction `TX-8698879b6979bfca0cd0226e`.
- В каталоге индекс единожды содержит `skills/learned/airworker-chatgpt-two-real-codex-writers-20261108.md@698d44b691c09d7a3e715ac330e206551b0db3b8846fec67e40b3e4698fcd165`, проверен SHA; `learn self load` RC0 `loaded`, квитанция `C:\ProgramData\AIR OS\State\air-worker-self-learning\gpt__devwork30-airworker-handoff-20261108\context-1db2335c10e1629c8af353f1.json` SHA `2157b9b0c60f240503bceef00e05faa3bc71f5ec9e521821229ec5276ea6ea46`.
- **APPLIED+LOADED != USED/EFFECT.** Новая сессия вправе загрузить тот же SHA под **своей** session key и измерить реальный `procedure_used`/outcome только при подтверждённом причинном эффекте. Не записывать фиктивный эффект. Собственное обучение — не подмена G07.

Исходные инструкции `skills/woody/SKILL.md` и `.codex-plugin/plugin.json` актуализированы под новый режим. В ранних исторических блоках PLAN/README прежняя схема помечена как история; при конфликте применять этот хендофф и верхний checkpoint.

## 4. Как начать следующую сессию

1. Старт AIR OS / AirWorker через принятую стартовую процедуру, **явный node SRVLM01**. Прочитать `skills/woody/SKILL.md`, этот хендофф и верх существующего `PLAN.md`, не заводить новый план.
2. Сверить **оба** `git HEAD / upstream / status --porcelain` (source и Wiki). Если дерево не CLEAN — проверить чужого writer и сохранённые receipts, не применять `git clean/reset`.
3. Выполнить `air-worker version`, `codex --version`, `claude --version`, `learn self status -json`, `tools/p0-progress.ps1`. Проверить exact owner, latest SHA и native `operational-goal status`. В source-only работе использовать отдельную собранную из **текущего чистого source** CLI в собственном D:-TEMP, не подменять установленную `rc.2`.
4. Сначала без новых моделей изучить уже принятые job/qa receipts. Для G07 восстановить две реальные отдельные chat sessions/модельные прерывания без повторного job. Не засчитывать два `read-only status` в одном процессе за требуемые межчатовые разрывы.
5. Держать оркестрацию как **ChatGPT координатор + 2 Codex code writers**, потом G06, G05, G08. Гейты G05/G06/G07/G08 обновлять только native, с точными receipt SHA. При отсутствии новой работы не создавать повторного запуска ради теста.
6. После окончания — source SHA и Wiki commit/push/remote readback, оба чистые, без уничтожения D:/R: чужих данных и без незапрошенного выпуска/установки.

**Никакой фоновой работы после хендоффа не обещано.** Новая сессия продолжает по фактическому live состоянию. Отсутствие доступа к чужим GPT-сессиям честно оставляет соответствующий G07 шаг `NOT_PROVEN`.
