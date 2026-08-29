---
name: run-worker-task
description: Запустить тяжёлую нероевую задачу через air-worker. По умолчанию подписанная заявка немедленно идёт через llm-queue; Telegram — только явный устаревший режим --approval telegram.
---

# air-worker — локальный исполнитель тяжёлых нероевых задач

`air-worker` — canonical plugin root для Claude Code/Anthropic и Codex; команды ниже
запускаются именно из этого корня. `../air-worker-codex` сохранён только как checkout
compatibility adapter для старых Codex-раскладок, запускает этот core и не является
release source. Core создаёт подписанную SQLite-заявку, повторно проверяет контракт
перед запуском и передаёт работу только
в существующий `llm-queue`; собственный HTTP-клиент, subprocess-очередь и ключ
OpenRouter не создаются.

Задайте на хосте `AIR_WORKER_RUNTIME` (каталог SQLite, результатов, metrics и
protocol JSONL). Если его нет, core использует `AIR_RUNTIME_ROOT`, затем
`%LOCALAPPDATA%/air-worker` на Windows или `$XDG_STATE_HOME/air-worker` на POSIX.
Старый `E:\-4-\air-worker` выбирается только как read-compatible migration fallback
при наличии прежнего state. Для host без Windows keyring задайте
`AIR_WORKER_HMAC_KEY` через его секрет-хранилище. Пути строятся `pathlib`.

## Обычный порядок: direct

```text
1. python skills/run-worker-task/scripts/worker.py types
2. python skills/run-worker-task/scripts/worker.py create openrouter-llm <file> \
       --param model=nvidia/nemotron-3.5-lightning:free \
       --param instruction="..." --privacy external
3. python skills/run-worker-task/scripts/worker.py status <id>; \
   python skills/run-worker-task/scripts/protocol.py
```

`create` по умолчанию ставит и исполняет задачу сразу. Он **не** вызывает Telegram,
не читает `foreign_listener_state`, не берёт общий bot lock и не зависит от listener.
Состояния core: `queued → running → done|failed|invalid`. Атомарное claim не даёт
исполнить один id повторно; восстановление после `failed` или прерванного вызова —
только новая подписанная заявка, не тихий replay.

Current release gate: direct execution fails closed until `llm-queue` exposes the
targeted JSON capability contract `run-job`, `show-job-json`. The direct path may
only atomically start its own queue id and poll that same id's redacted JSON
receipt; the historical global `run --limit` may process another request and is
forbidden here. See `docs/GOAL.md` for the upstream blocker.

## Контракт и границы

- `task_type`, параметры, путь и SHA-256 материала подписаны HMAC. Подпись, digest,
  registry и executor-validator проверяются перед запуском.
- `--privacy local` подписывается и запрещает внешний `openrouter-llm`; локальная
  Qwen остаётся через llm-queue. Уровень приватности определяет модель, не место
  запуска процесса.
- `AIR_WORKER_FREE_ONLY=1` по умолчанию: OpenRouter принимает только `:free`;
  локальный router остаётся финальным billing guard. Лимиты размера, ввода,
  timeout и concurrency выполняют core/исполнитель/llm-queue.
- Результат, status и строка metrics содержат id, тип, модель, счётчики и код
  исхода; prompt, секреты, input/output paths и текст материала не пишутся в
  protocol или metrics. Protocol — append-only JSONL с отдельной `external` ногой.
- На текущем AIR OS product knowledge и roadmap принадлежат vault
  `E:\-5-\011_Plugins\AirWorker_Wiki`; runtime, заявки, результаты, metrics,
  protocol и секреты туда не копируются. На другом хосте координату находят через
  AIR Storage registry, а не предполагают наличие диска `E:`.
- Повторы/ретраи, backoff, cancellation, durable resume и 429 handling не являются
  контрактом air-worker; air-worker не запускает второй независимый процесс.
  Платная эскалация не молчаливая: `ladder.escalate(..., reason)` требует
  причины.

## Legacy Telegram (не default)

Только если нужна человеческая кнопка, вызовите `create ... --approval telegram`,
затем `listener.py --once 30`. Только этот режим проверяет свой lock и чужой
`approve_listener.py`, читает callback `awrun:<id>`/`awno:<id>` и шлёт bot API.
Listener — тонкий адаптер: после callback он вызывает тот же `worker.execute_request`.
Чужой chat_id, просроченная, повторная или сломанная заявка не запускаются.

## Цикл

```text
цель   — получить результат ровно для подписанного контракта
гейт   — selftest.py + signature/registry/privacy/size/free-only + llm-queue status
предел — одна попытка core на id; очередь применяет свои bounded retry; ошибка → новая заявка
показ  — один status/result/metrics/protocol summary
```

`python skills/run-worker-task/scripts/selftest.py` — офлайн-проверка (включая
direct, legacy mock, protocol, malformed router и POSIX-path). Живой вызов отдельно:
`python skills/run-worker-task/scripts/live_check.py`.
Selftest не заменяет проверку живого маршрута.

## Миграция 0.3.0

Старый порядок `lock → create → listener` больше не обязателен: без флага
`--approval telegram` заявка запускается напрямую. Telegram и его замки сохранены
для совместимости, но помечены legacy. До установки новой версии существующий
Claude plugin продолжает старое поведение; обновление/установка — отдельное действие
ЛПР, не часть этого изменения.

## Лестница исполнителей (TASK-OBS-0054)

`scripts/ladder.py` и `ladder_priors.json` дают уровни 0–5 поверх того же
подписанного local contract. Уровни 1–3 используют `llm-queue`; 4–5 идут в
оркестратор. `escalate(claim, level, reason)` без причины отклоняется;
`choose_start_level()` остаётся на 0 без достаточной статистики; `verify()`
даёт независимую проверку для chronology/decision/principle/risk. Реестры дел и
секреты OpenRouter этот модуль не читает.
