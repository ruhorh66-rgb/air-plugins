# Самообучение AirWorker · паритет трёх хостов · 15.11.2026

Исполнитель: Dev Work 35. Основание: указание ЛПР 15.11.2026 «для AirWorker нужно
3 плагина: Codex, Cloud и Hermes»; решение куратора: логика в бинарнике, плагин
хоста — тонкий переходник. Инвентаризация снята только чтением, на SRVLM01.

## 1. Что установлено сейчас

| Хост | Установка | Версия | Хуки | Ссылки на самообучение |
|---|---|---|---|---|
| Claude Code | `R:/-4-/claude-home/plugins/cache/air-plugins/air-worker/0.11.11-rc.5` (рядом rc.2), включён | 0.11.11-rc.5 | `hooks/hooks.json`: SessionStart, UserPromptSubmit, Stop, Pre/PostToolUse, SubagentStart/Stop → `bin/air-worker.exe hook <Event>` + guards Дятла | Stop → `run_completed`; SessionStart/UserPromptSubmit → подача процедур и учёт loaded |
| Codex | `R:/-4-/codex-home/plugins/cache/air-plugins/air-worker/0.11.11-rc.5`, `config.toml`: enabled | 0.11.11-rc.5 (тот же пакет) | `.codex-plugin/plugin.json` хуков не объявляет; `hooks/hooks.json` лежит в пакете, флага хуков в `[features]` нет — вероятно не активны (запуском не проверено) | Только через бинарник при вызове CLI из оболочки |
| Codex (старый) | `F:/-7-/air-worker-codex` | 0.3.0 | нет; `scripts/run_worker.py` + skill `run-worker-task` | нет |
| Hermes | `R:/-4-/hermes/plugins/air-worker` | 0.10.10 (пакет rc.5 несёт более новый `.hermes/plugins/air-worker` и профили `airworker-hermes-v1-enforce/shadow`) | pre_llm_call, pre/post_tool_call, pre_verify, transform_llm_output, subagent_start/stop, on_session_start/end | В `hooks.py`/`adapter.py` упоминаний learn — 0 |

## 2. Что нужно каждому плагину для паритета

Бинарник уже одинаков для всех трёх: захват отказов CLI (А1), счётчик вызовов,
`learn` CLI. Ниже — только хост-зависимые части.

| Функция | Claude Code | Codex | Hermes | Объём |
|---|---|---|---|---|
| Хост вызова в счётчике (`AIR_WORKER_HOST`) | работает по признаку `CLAUDECODE=1`; объявление в адаптере — по желанию | не сделано: строка `shell_environment_policy` в `config.toml` (§3.2) | не сделано: одна строка в `adapter.py` (§3.1) | малый, гейт ЛПР |
| Захват завершённого хода (`run_completed`) | работает (Stop) | не сделано: нужен хук завершения хода Codex или явный вызов `air-worker learn` из навыка; сначала проверить, поддерживает ли установленный Codex хуки | не сделано: `on_session_end`/`transform_llm_output` → тот же вход, что Stop | средний на каждый хост |
| Подача процедур (А4) | работает (SessionStart/UserPromptSubmit), отбор по свежести | не сделано: без хука — только через навык или AGENTS-вставку | не сделано: `pre_llm_call` → `learn self load` | средний |
| Учёт loaded/used | loaded — работает; used — по наблюдаемому эффекту операции AirWorker | не сделано | не сделано | средний |
| Доставка сводки ЛПР (А5) | не применимо: доставка идёт не через хост сессии | не применимо | канал доставки — Hermes Desktop Gateway (решение ЛПР 15.11), проект подключения отдельно | средний |
| Обновление установленного плагина до релиза | установлен rc.5 | установлен rc.5 | установлен 0.10.10 — сначала догнать версию пакета | малый, гейт установки |

Оценка: всё вместе — отдельный шаг плана порядка трёх малых релизов (по одному на
хост), после А1/А4. Для Codex первым делом нужен факт: работают ли хуки плагина в
установленной версии Codex; без него объём не оценить точнее.

## 3. Подготовленные изменения (не применены, гейт ЛПР)

### 3.1 Hermes — `.hermes/plugins/air-worker/adapter.py`

```diff
 def _invoke(argv: list[str], product: Path, timeout: int) -> dict[str, Any]:
     started = time.monotonic()
     try:
         done = subprocess.run(
             argv,
             cwd=str(product),
+            env={**os.environ, "AIR_WORKER_HOST": "hermes"},
             capture_output=True,
```

Плюс тест в `tests/test_plugin.py`, что дочерний процесс получает
`AIR_WORKER_HOST=hermes`. Профиль и настройки Hermes не меняются; установка —
через штатную поставку плагина, не правкой `R:/-4-/hermes/plugins`.

### 3.2 Codex — `R:/-4-/codex-home/config.toml`

```toml
[shell_environment_policy]
set = { AIR_WORKER_HOST = "codex" }
```

Секции `shell_environment_policy` в текущем `config.toml` нет. Перед применением —
проверить по документации установленной версии Codex, что `set` не отключает
наследование остальных переменных (`inherit` по умолчанию).
