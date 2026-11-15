# Самообучение AirWorker · А1 и А2 · передача на интеграцию · 16.11.2026

От: Dev Work 35. Кому: writer канонической D-линии AirWorker (на 15.11 — Dev Work32,
`D:\Devbox\AirWorker\work\runtime-discovery-20261114`, ветка
`repair/runtime-discovery-20261114` @ `b0a2b2c`). Через куратора (Voody) и ЛПР.

Статус: **исходные кандидаты к интеграции, не приёмка.** Вердикт судьи
предварительный: автор кода — Claude, судья — Codex через `codex exec --sandbox
read-only`; штатный `air-worker review source` к этому коду не подходит (он требует
автора Codex и судью Anthropic). Шаг — контур S2 (AIR_VIBECODING инв. 15,
DEV-135 §0), в состав S0 не подмешивать.

## 1. Что вливать

| Ветка | HEAD | Родитель | Что делает | План |
|---|---|---|---|---|
| `dev/airworker-a1-cli-signal-from-rc5` | `0ec419b73c6f1ccd047504706e62cabb11e3e03b` (код); docs до `HEAD` ветки | `7e71303` (rc.5) | А1: сигнал обучения CLI и счётчик вызовов | N-121; знаменатель Б1 — N-125 |
| `dev/airworker-a2-reviewer-stage-from-rc5` | `42c155a105cd94fcca813667eb9690acdacb0ea2` | `7e71303` | А2.1 стадия отказа ревьюера; А2.2 отложенный повтор детерминированного отказа | N-110 |

Проверенная связка (только для QA, не для интеграции): `qa/airworker-a1-a2-on-rc5`
= `a60e01ca3586a4ebb530762c32ec327d676bd0fa` = rc.5 + `merge --no-ff 0ec419b` +
`merge --no-ff 42c155a`.

Где взять: бандл `E:\-4-\air-worker\source-bundles\dw35-selflearning\` (последний
файл `…-qa-a60e01c.bundle` и следующие; `.sha256` рядом; `git bundle verify`). Пуша
нет: `air-plugins` публичный, публикация — решение ЛПР.

**Порядок слияния.** В каноническую ветку: сначала А1 (`0ec419b` или HEAD ветки А1,
если нужны docs), затем А2 (`42c155a`). Ветки не делят файлов. Пробное слияние
(`git merge-tree --write-tree b0a2b2c a60e01c`) — чисто, дерево `7c805ec6…`.

## 2. Файлы

А1 (`7e71303..0ec419b`): `cmd/main.go`, `cmd/self_cli_failure.go`,
`cmd/self_cli_signal.go` (новый), `cmd/self_learning.go`; тесты
`cmd/self_cli_signal_test.go` (новый), `cmd/self_cli_failure_test.go`,
`cmd/self_cli_private_key_test.go`; docs `docs/learning/*`.

А2 (`7e71303..42c155a`): `cmd/learn_adapter.go`, `cmd/learn_shared.go`,
`cmd/learn_shared_deferred_recovery.go`, `cmd/learn_reviewer_stage.go` (новый),
`cmd/learn_reviewer_stage_test.go` (новый).

**Единственное место возможного конфликта** с D-линией: `cmd/main.go` (А1 добавляет
в `main()` вызов `ownCLIRunID`/`recordOwnCLIInvocation` и передаёт `runID` в
`autoCaptureOwnCLIExit`). В пробном слиянии с `b0a2b2c` — авто-слияние без
конфликта. `cmd/learn.go`, версия, манифесты, `PLAN.md`, `plan/` не тронуты.

## 3. Поведение

**А1.** Событие обучения не создаётся только при коде 2 без проверенной причины
(`-h` попадает сюда же). Сохранены: `validate-run-config-missing`,
`validate-plan-missing`, FAIL судьи (код 1, инв. 15), прочие ненулевые коды.
Счётчик `<runtime>/cli-invocations/<UTC-дата>.jsonl`: на каждый операторский вызов
строка `run_id` (тот же, что у квитанции отказа), `command`, `exit_code`,
`help_token`, `host`/`host_source` (`AIR_WORKER_HOST` = declared; `CLAUDECODE=1` =
marker), `version`, время до секунды; аргументов нет. Не считаются `hook`,
`learning-adapter`, `internal-self-cli-replay`, `tray`, `learn review-batch`, дочерний
процесс R02. `air-worker learn self cli-stats [-days N]`: всего, `by_host`,
`by_command` и `pilot` (классы ошибки вызова `plan`, `plan node *`, `plan-lint`,
`unknown`; гейты в цель Б1 не входят).

**А2.** Ошибка ревьюера без известного диагноза хранится как
`reviewer_<stage>_failed; detail withheld`, стадии `product|settings|input|catalog|
invoke|answer`; текст не попадает в журнал. Восстановление не отдаёт на немедленный
повтор детерминированный отказ (стадии product/settings/catalog и фиксированные
коды), а повторяет его не раньше `deterministicReviewerRetryDelay = 1h`; потолок
попыток rc.5 не менялся.

## 4. Проверки

| Проверка | Результат |
|---|---|
| Адресные тесты А1 (A1*, N110, N117, TrustedCause, ReleaseSlice, Preflight) | PASS |
| Адресные тесты А2 (TestA2* 5, LearningFailure, Deferred) | PASS |
| gitleaks 8.30.1 по `7e71303..0ec419b` и `7e71303..b0a2b2c` | 0 находок |
| Чистый полный прогон, база `7e71303` | vet 0; 1 флейк (`TestSemanticReviewerRegistersReceipt`); 1856 с |
| Чистый полный прогон, связка `a60e01c` | vet 0; 1 флейк (`TestJudgeSourceIdentityRejectsHiddenIndexFlags`, 5/5 отдельно); 1893 с |
| Codex, А1 | 5 раундов, последний (`0ec419b`) PASS, новых находок нет |
| Codex, А2 | 4 раунда; Р3 подтвердил: потолок попыток — свойство rc.5; Р4 (`42c155a`, задержка 1 ч) PASS |

Подробности: `RC5_BASELINE_TEST_FAILURES_20261115.md`, вердикты — в scratchpad
сессии Dev Work 35 (временный каталог, не хранилище).

## 5. Что остаётся до установки

1. Вливание writer'ом и номер **1.0.0-rc.1** одной операцией в каноническом дереве
   (решение ЛПР 15.11). Из веток как есть не собирать: константа `version` там — rc.5.
2. Штатный независимый судья на интегрированном SHA.
3. Чистый полный прогон на интегрированном SHA (короткий `GOTMPDIR`).
4. Сборка CLI и tray с `-buildvcs`, пакет по DEV-070, откат на установленную rc.5
   (каталоги плагина 0.11.11-rc.5 в кэшах Claude и Codex сохраняются).
5. «Да» ЛПР на установку точных SHA исходника и артефакта.
6. После установки — 7 дней сбора; первое число Б1: `learn self cli-stats -days 7`,
   строка `pilot`; доля `error` в `reviews` за 7 дней — цель А2 < 10%.

Откат данных не нужен: каталог `cli-invocations` прежний бинарник не читает, коды
`reviewer_<stage>_failed` он видит как обычную ошибку.

## 6. Сопутствующее

- Переходники хостов (`AIR_WORKER_HOST`), diff'ы не применены, гейт ЛПР:
  `SELFLEARNING_HOST_PARITY_20261115.md` §3 — одна строка `env=` в
  `.hermes/plugins/air-worker/adapter.py` и секция `shell_environment_policy` в
  `config.toml` Codex. Хуки хостов для захвата хода, подачи и loaded/used — S2,
  только проект.
- Заявки на native-узлы (А1–А6, Б1–Б5, P3-пробел покрытия):
  `SELFLEARNING_AB_NODE_REQUESTS_20261115.md` — заводятся writer'ом одной операцией.
- Проект А2/А3 и план свёртки 32 дублей: `SELFLEARNING_A2_A3_DESIGN_20261115.md`
  (А3 в код не взят до решения ЛПР по маршруту кодинга).
- Заявка в air-modules: `AIR_MODULES_UNJUDGED_VERDICT_REQUEST_20261115.md`.
- Старые бинарники rc.2 и 0.11.9 в кэшах продолжают писать события старым способом —
  убрать при установке (А2.3, решение установки).
