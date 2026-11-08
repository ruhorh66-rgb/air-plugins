# AirWorker · Dev Work 30 → следующая GPT-сессия · 08.11.2026

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
