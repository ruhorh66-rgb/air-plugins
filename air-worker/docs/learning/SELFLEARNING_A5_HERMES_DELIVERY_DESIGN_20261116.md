# А5 · проект доставки сводок и очереди через Hermes Desktop (16.11.2026)

Проект только для чтения, по AIR_PRODUCT_PRODUCTION_PLAN §5.4.2–5.4.3. В Hermes ничего не
записано, ничего не запущено, второй gateway не создавался. `.env`, `auth.json` и
`config.yaml` не читались. Узел N-034, строка А5: «delivered с readback».

## 1. Исходное состояние (наблюдение 16.11.2026, по файлам и списку процессов)

| Поле | Значение | Как установлено |
|---|---|---|
| Узел | локальная машина ЛПР (Windows 10) | — |
| Hermes | 0.23.0, commit `2bd378e` (05.11.2026) | `git describe` в `R:\-4-\hermes\hermes-agent` |
| Штатный launcher | `R:\-4-\hermes\bin\hermes.exe` | первый в PATH |
| Чужой launcher в PATH | `D:\DevBox\AirLegal\qa\n197-hermes-review-home\bin\hermes.cmd` — QA-home Legal | `Get-Command -All`; поэтому адресовать только полным путём и `-p default` |
| Профиль по умолчанию | `default`, effective home = `HERMES_HOME` = `R:\-4-\hermes` | переменная окружения и аргумент работающего gateway |
| Прочие профили | airbuch, airfinance, airhr, airlegal, airmarketing, airpto, airsmeta, airsupply, airvera | каталог `profiles` |
| Gateway | один процесс: `python -m hermes_cli.main --profile default gateway run --quiet` (PID 7408) | список процессов |
| Плагины в home | `air-worker` 0.10.10, `curator-approval-notify` 0.1.0 (Telegram при ожидании одобрения в Desktop), `curator-desktop-patrol`, `curator-session-link`, `homeassistant` | `plugins/*/plugin.yaml` |

`--help` установленной версии не запускался: §5.4.3 п.2 предупреждает о побочных
записях launcher. Справка взята из исходников того же commit (`hermes_cli/send_cmd.py`,
`website/docs`). Вывод `--help` живого launcher — **NOT_PROVEN**.

## 2. Маршруты

| Маршрут | Через Gateway | Попадает в Desktop | Вывод |
|---|---|---|---|
| `hermes send --to <platform>` | нет: для Telegram/Discord/Slack шлёт по bot-token сам, gateway не нужен; читает `.env` в окружение процесса | нет, только во внешний мессенджер | не подходит |
| Уведомления фоновых процессов (`tui_gateway/session_notifications.py`) | да | да, но только для процессов своей сессии | не канал доставки |
| **Cron без LLM (`no_agent`) с `deliver=bot-chat`** | планировщик работающего gateway | да: канонический Bot Chat профиля, его видно в Desktop | **рекомендуется** |

Свойства Bot Chat по документации 0.23.0 (`developer-guide/cron-internals.md`,
`guides/automate-with-cron.md`, `guides/cron-script-only.md`):
- псевдоплатформа только этой машины, адрес `bot-chat` — свой профиль; адрес
  `bot-chat:<profile>` — другой;
- сообщение приходит как настоящий входящий ход: бот его читает и отвечает. Это
  **стоит один ход модели** на доставку;
- без работающего владельца Bot Chat запуск записывается `delivery_failed`,
  повторной доставки нет, планировщик не становится вторым писателем;
- скрипт без LLM доставляется вне очереди: запуск остаётся `delivery_queued`;
  окончательный итог есть только в квитанции целевого профиля;
- скрипты лежат только в `<home>/scripts/`, учётные данные процесса Hermes не наследуют;
- пустой stdout — тихий тик, без сообщения.

## 3. Предлагаемая схема

1. **Источник.** Существующие команды AirWorker без LLM:
   - `air-worker curator digest -all -json` — вехи, ЖДЁТ ДА, эффект обучения, «сводка 10:00 SGT»;
   - `air-worker learn self cli-stats` — после Б1.

   Новый формат не вводится.
2. **Скрипт** `R:\-4-\hermes\scripts\airworker-digest.py`:
   - вызывает установленный `air-worker` полным путём;
   - печатает короткий русский текст: вехи, число и первые пункты ЖДЁТ ДА, ссылку на полную сводку;
   - если SHA сводки совпадает с последним отправленным, печатает пусто (тик без хода модели);
   - секретов и приватных ID не печатает.
3. **Задание**: `cronjob(create, schedule="0 10 * * *" по SGT, script=..., no_agent=true,
   deliver="bot-chat", name="airworker-digest")`. Профиль — `default`; второго gateway
   нет: тикает уже работающий планировщик.
4. **Readback «delivered»**: квитанция доставки целевого профиля по receipt ID из
   `last_delivery_queued` плюс запись хода в Bot Chat. Пока точная команда чтения
   этой квитанции на 0.23.0 не проверена — **NOT_PROVEN**. `delivery_queued` сам по
   себе не доказывает доставку.
5. **Откат**: `hermes -p default cron remove <id>` и удаление скрипта; остальное в
   home не меняется.

## 4. Гейты и открытые вопросы

- **Запись в Hermes** (скрипт в `scripts/` и cron-задание) — гейт ЛПР. Сама запись —
  через штатный интерфейс (`cronjob` из чата Desktop или `hermes -p default cron ...`).
  Перед ней — preimage списка заданий.
- **Ход модели на каждую доставку.** Это стоимость. Альтернатива без хода — внешний
  мессенджер; тогда это уже не Desktop. Решение ЛПР.
- **Пересечение с `curator-approval-notify`.** Он шлёт в Telegram об одобрениях
  Desktop. А5 его не заменяет и не дублирует.
- **Плагин `air-worker` 0.10.10** отстаёт от пакета. Для А5 он не нужен: скрипт
  вызывает CLI напрямую.
- **Существующие cron-задания** не просматривались: в них могут быть приватные промпты.
  Перед созданием нужно сверить имена, чтобы не было двойной сводки.
