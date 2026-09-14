# GPT/OpenAI audit → Anthropic development session

Контекст: параллельная GPT-сессия проверяет Air Worker как OpenAI/Codex executor и как основу для ChatGPT+RDC. ЛПР разрешил совместную работу и потребовал явно передавать замечания второй стороне.

## Найдено для шага 58 (НЕ входит в 0.10.0)

Исправление подготовлено и сохранено в `.woody/GPT_FUTURE_STEP58_AUDIT.patch`, но сознательно вынесено из релиза 0.10.0:
- Codex executor сейчас запускается с `-s read-only`; для coding-runner нужен `workspace-write`.
- Общий prompt неверно говорит Codex, что локальные команды невозможны; граница возможностей должна зависеть от runner.
- Codex JSONL parser должен различать non-fatal `item.completed(type=error)` и terminal errors.
- Codex-процесс не должен получать `CLAUDE_CODE_OAUTH_TOKEN` через общий `runnerEnv()`.

## Уже принято в 0.10.0

- `hooks/hooks.json` приведён к схеме, которую принимает и Codex: служебные `_`-поля убраны в `description`.
- Claude/Codex manifests сведены к версии 0.10.0.

## Решения ЛПР 14.09.2026

- Планировщик и исполнитель должны быть разных семейств: Claude executor → OpenAI/Codex planner; OpenAI/Codex executor → Anthropic planner.
- Третий вариант — GPT в чате + Remote Desktop Commander. Его нельзя притворять локальным CLI: Air Worker должен выдавать task/handoff-пакет, чат исполняет через RDC, после чего Air Worker независимо запускает judge/drift. Это отдельная будущая работа и в 0.10.0 не входит.
