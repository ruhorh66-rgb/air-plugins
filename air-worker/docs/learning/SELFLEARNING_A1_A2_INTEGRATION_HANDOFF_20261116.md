# Самообучение AirWorker · А1, А2 (с А2.4), А3 · передача на интеграцию · 16.11.2026

От: Dev Work 35. Кому: writer канонической D-линии AirWorker (на 16.11 — Dev Work32,
`D:\Devbox\AirWorker\work\runtime-discovery-20261114`, ветка
`repair/runtime-discovery-20261114` @ `b0a2b2c`). Через куратора (Voody) и ЛПР.

Статус: **исходные кандидаты к интеграции, не приёмка.** Вердикты судьи
предварительные: автор кода — Claude, судья — Codex через `codex exec --sandbox
read-only` (решение ЛПР 16.11 «дев ворк 35» по маршруту кодинга); штатный
`air-worker review source` к этому коду не подходит (он требует автора Codex и
судью Anthropic). Шаги — контур S2 (AIR_VIBECODING инв. 15, DEV-135 §0), в состав
S0 не подмешивать. Публикации нет: `air-plugins` публичный, невыпущенные ветки туда
не пушатся (решение ЛПР 16.11); защита — бандлы на E:.

## 1. Что вливать и в каком порядке

| # | Ветка | HEAD | От | Что делает | План |
|---|---|---|---|---|---|
| 1 | `dev/airworker-a1-cli-signal-from-rc5` | код `0ec419b`, docs до HEAD ветки | `7e71303` (rc.5) | А1: сигнал обучения CLI и счётчик вызовов (`learn self cli-stats`) | N-121; знаменатель Б1 — N-125 |
| 2 | `dev/airworker-a2-reviewer-stage-from-rc5` | `b355dd1c115eb205ef37d2a938143206448a2054` | `7e71303` | А2.1 стадия отказа; А2.2 отложенный повтор детерминированного отказа (1 ч); **А2.4** имя задания ревьюера на событие, восстановление результата того же события, политика хранения квитанций | N-110 |
| 3 | `dev/airworker-a3-one-class-from-rc5` | `7db4bbf465d0ae99cf2aa08dcaaba2d76df23e20` | `7e71303` | А3.1 один класс — одна процедура; А3.2 вердикт «judge not configured» не материал урока | N-110 |

**Порядок слияния:** А1 → А2 → А3 (каждое `--no-ff`).

**Пробная интеграция (16.11, временное дерево, удалено):** rc.5 + А1 (`09c4483`) +
А2 (`b355dd1`) + А3 (`7db4bbf`) = `14a651c`; все три слияния без текстового
конфликта. **Одна обязательная правка после слияния А3:** А2.4 добавил первым
параметром `jobKey` в `learningReviewerInvoke`, а тест А3 написан от rc.5:

```diff
-	learningReviewerInvoke = func(root, prompt, outputSchema, model, effort string) (string, error) {
+	learningReviewerInvoke = func(jobKey, root, prompt, outputSchema, model, effort string) (string, error) {
```

в `cmd/learn_adapter_one_class_test.go:13` (единственное место). После неё
`go vet` 0 и набор LearningReviewer|Reviewer|Learning|Adapter|SharedLearning|
Preflight|N102|N110|N117|N121|SelfLearn|JobReceipt|A1|A2|A3 — `ok` (94 с).

Пробное слияние с D-линией: `git merge-tree --write-tree b0a2b2c <ветка>` — чисто
для А1 и А2; общий с D-линией файл только `cmd/main.go` (А1), сливается автоматически.

Где взять: бандлы `E:\-4-\air-worker\source-bundles\dw35-selflearning\`
(последний — `…-a24ret-b355dd1.bundle`, SHA-256 рядом в `.sha256`; `git bundle verify`).

## 2. Файлы по веткам

А1 (`7e71303..0ec419b`): `cmd/main.go`, `cmd/self_cli_failure.go`,
`cmd/self_cli_signal.go` (новый), `cmd/self_learning.go`; тесты
`cmd/self_cli_signal_test.go` (новый), `cmd/self_cli_failure_test.go`,
`cmd/self_cli_private_key_test.go`; `docs/learning/*`.

А2 (`7e71303..b355dd1`): `cmd/learn_adapter.go`, `cmd/learn_shared.go`,
`cmd/learn_shared_deferred_recovery.go`, `cmd/learn_reviewer_stage.go` (новый),
`cmd/learn_reviewer_retention.go` (новый); тесты `cmd/learn_reviewer_stage_test.go`,
`cmd/learn_reviewer_job_test.go`, `cmd/learn_reviewer_retention_test.go` (новые) и
ведущий `jobKey` в моках `learn_adapter_test.go`, `learn_adapter_reuse_test.go`,
`learn_adapter_retention_test.go`, `learn_reviewer_reliability_test.go`,
`self_preflight_production_template_test.go`.

А3 (`7e71303..7db4bbf`): `cmd/learn_adapter.go`, `cmd/learn_shared.go`; тесты
`cmd/learn_adapter_one_class_test.go` (новый), `cmd/learn_adapter_retention_test.go`,
`cmd/learn_adapter_reuse_test.go`.

Не тронуты: `cmd/learn.go`, `cmd/job_receipt.go`, версия, манифесты, `PLAN.md`, `plan/`.

## 3. Поведение

**А1.** Событие обучения не создаётся только при коде 2 без проверенной причины
(`-h` попадает сюда же). Сохранены: `validate-run-config-missing`,
`validate-plan-missing`, FAIL судьи (код 1, инв. 15), прочие ненулевые коды.
Счётчик `<runtime>/cli-invocations/<UTC-дата>.jsonl` (run_id, command, exit_code,
help_token, host/host_source, version; без аргументов); `learn self cli-stats`:
всего, by_host, by_command, `pilot` (классы ошибки вызова).

**А2.** Ошибка ревьюера без известного диагноза → `reviewer_<stage>_failed; detail
withheld`; детерминированный отказ повторяется не раньше 1 ч.
**А2.4** (корень ~980 `reviewer_failed`: ревьюер с фиксированным именем задания
блокировался собственной квитанцией с 09.11.2026): задание `learning-<16 hex SHA
события>`, полный SHA события в квитанции; завершённая квитанция того же события с
совпадающими продуктом, ролью, каноническим путём и SHA вывода — результат берётся
повторно; иначе `reviewer_job_evidence_mismatch`; RUNNING — прежняя защита
(`reviewer_job_evidence_exists`/`reviewer_job_busy`). `job_receipt.go` не менялся.
**Хранение:** только сверх 200 квитанций ревьюера; переносятся (не удаляются)
DONE/FAILED старше 7 дней (и по старту, и по времени файлов), с целым выводом и
`phase=complete`, под замком задания, через `os.Root` (`Link` без замены, затем
снятие старого имени); трасса `<runtime>/reviewer-job-archive.jsonl` до и после,
повисшие «moving» сверяются при следующем проходе.

**А3.** Если у класса есть update-eligible процедура, урок обязан явно обновить
именно её (`update_target` этого класса + точный `pre_sha256`); ответ без цели →
`reviewer_class_update_required`; повтор применённого текста → `{}`; update чужого
класса — отказ; новый класс → новый файл. Семейство — `<target>` или
`<target>-<16 hex>.md`. Вердикт «unjudged / judge not configured» точной пустой формы
передаётся ревьюеру как `{status: unjudged, configured: false}`.
Решение по N102 (куратор 16.11, вариант (а), основание — решение ЛПР 04.11 и инв. 15):
старый тест N102 переписан; защита от бесследной перезаписи сохранена отдельным
`TestN102NoTraceOverwriteOfExistingProcedureIsImpossible`.

## 4. Проверки

| Проверка | Результат |
|---|---|
| Чистый полный прогон, база `7e71303` | vet 0; 1 флейк; 1856 с |
| Чистый полный прогон, связка А1+А2 (до А2.4) `a60e01c` | vet 0; 1 флейк (5/5 отдельно); 1893 с |
| Пробная интеграция А1+А2+А3 `14a651c` (+1 строка) | vet 0; набор тестов обучения ok |
| gitleaks по А1 и линии Dev Work32 | 0 находок |
| Codex А1 | 5 раундов, последний PASS |
| Codex А2.1/А2.2 | 4 раунда, последний PASS |
| Codex А2.4 (i–iii) | 2 раунда, PASS; «evidence protection unchanged» |
| Codex хранение квитанций | 6 раундов, последний PASS; «never deletes or replaces evidence: CONFIRMED» |
| Codex А3.1/А3.2 | 3 раунда, последний PASS; «N102 protection: PRESERVED», старые тесты DECIDED-RULE |

## 5. Что остаётся до установки

1. Вливание writer'ом в порядке А1 → А2 → А3 и правка одной строки теста (§1);
   номер **1.0.0-rc.1** одной операцией в каноническом дереве (решение ЛПР 15.11).
2. Чистый полный прогон на интегрированном SHA (короткий `GOTMPDIR`).
3. Штатный независимый судья на интегрированном SHA.
4. Сборка CLI и tray с `-buildvcs`, пакет по DEV-070, откат на rc.5 (кэши Claude и
   Codex 0.11.11-rc.5 и слот отката апдейтера сохраняются).
5. «Да» ЛПР на установку точных SHA исходника и артефакта.
6. После установки: 7 дней сбора → `learn self cli-stats -days 7` (`pilot`, Б1);
   доля `error` в `reviews` за 7 дней (А2, цель < 10%); свёртка 32 дублей — отдельный
   гейт ЛПР, только после установленного А3.1.

## 6. Эксплуатационные факты 16.11.2026

- Ревьюер разблокирован переносом квитанции 09.11 в
  `…\airworker-0119-n093-worker-b-20261018\air-worker\.woody\jobs-archive\20261116-reviewer-unblock\`
  (по «да» пользователя): первый следующий разбор (`EV-48e3d0390bb60b3b1e1640ce`)
  дошёл до модели, `no_candidate`, `phase=complete`; новая квитанция с прежним именем
  снова блокирует — до установки А2.4 так и останется.
- Кэш Claude: каталог `0.11.11-rc.2` удалён (квитанция со SHA-256 всех 894 файлов —
  `E:\-4-\air-worker\cleanup-receipts\20261116-claude-cache-air-worker-0.11.11-rc.2.csv`);
  слот отката апдейтера с rc.2 и staging rc.4 сохранены; 0.11.9 в кэшах нет.
- Старые бинарники события больше не пишут: последний захват rc.2 — 11.11, 0.11.9 — 09.11.

## 7. Сопутствующее

- Переходники хостов (`AIR_WORKER_HOST`), не применены, гейт ЛПР:
  `SELFLEARNING_HOST_PARITY_20261115.md` §3; хуки хостов — S2, только проект.
- Заявки на native-узлы (А1–А6, Б1–Б5, P3): `SELFLEARNING_AB_NODE_REQUESTS_20261115.md`.
- Проект А2/А3 и план свёртки дублей: `SELFLEARNING_A2_A3_DESIGN_20261115.md`.
- Заявка в air-modules: `AIR_MODULES_UNJUDGED_VERDICT_REQUEST_20261115.md`.
- Базовые падения rc.5: `RC5_BASELINE_TEST_FAILURES_20261115.md` (43/44 — окружение).
- В работе: А3.3 (CLI «судья не настроен → не оценено»), А3.4 (типизированный FAIL судьи).
