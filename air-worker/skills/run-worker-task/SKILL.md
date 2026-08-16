---
name: run-worker-task
description: Запустить тяжёлую нероевую задачу через air-worker. По умолчанию подписанная заявка немедленно идёт через llm-queue; Telegram — только явный устаревший режим --approval telegram.
---

# air-worker — локальный исполнитель тяжёлых нероевых задач

Один Python core обслуживает Claude Code/Anthropic и Codex. Он создаёт подписанную
SQLite-заявку, повторно проверяет контракт перед запуском и передаёт работу только
в существующий `llm-queue`; собственный HTTP-клиент, subprocess-очередь и ключ
OpenRouter не создаются.

Задайте на хосте `AIR_WORKER_RUNTIME` (каталог SQLite, результатов и protocol JSONL).
На Windows сохраняется прежний runtime как fallback; на POSIX fallback —
`~/.air-worker`. Для host без Windows keyring задайте `AIR_WORKER_HMAC_KEY` через
его секрет-хранилище. Пути в новом core строятся `pathlib`, не буквами дисков.

## Обычный порядок: direct

```text
1. python worker.py types
2. python worker.py create openrouter-llm <file> \
       --param model=nvidia/nemotron-3.5-lightning:free \
       --param instruction="..." --privacy external
3. python worker.py status <id>; python protocol.py
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
- Повторы/ретраи ограничены llm-queue; air-worker не запускает второй независимый
  процесс. Платная эскалация не молчаливая: `ladder.escalate(..., reason)` требует
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

`python selftest.py` — офлайн-проверка (включая direct, legacy mock, protocol,
malformed router и POSIX-path). Живой вызов отдельно: `python live_check.py`.
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
