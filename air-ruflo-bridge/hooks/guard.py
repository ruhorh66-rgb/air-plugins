#!/usr/bin/env python3
"""air-ruflo-bridge: запрет обхода обвязки — механизмом, а не текстом в SKILL.md.

ПОЧЕМУ ЭТО В ПЛАГИНЕ, А НЕ В ГЛОБАЛЬНОМ ХУКЕ. Указание ЛПР 08.08.2026: «не скилл,
а плагин — это должна быть жёсткая конструкция для разных платформ». Правило,
живущее в `SKILL.md`, читает только Claude Code и только если сессия открыла скилл;
правило, живущее в глобальном `settings.json` контура, не поедет на другую машину
вместе с плагином. Конструкция, которая едет с плагином, работает в обоих случаях.

Глобальная проверка `vibecoding_guard.check_hive_direct` при этом остаётся: она
страхует сессии, у которых плагин не установлен. Совпадение сознательное, а не
недосмотр — два предупреждения дешевле одного пропуска.

ЧТО ЗАПРЕЩАЕТСЯ. Прямой запуск роя мимо `run_task.ps1`: `hive-mind spawn`,
`hive-mind task`, `autopilot enable`, `swarm start`. Основание — `ERR-2026-000192`:
прямой `hive-mind spawn --claude` поднимает Queen-сессию, но НЕ заводит воркеров и
НЕ кладёт задачу в очередь роя. Снаружи похоже на рой, фактически — одна дорогая
сессия. Именно так рой не выполнил ни одной задачи за всё время своей работы:
18 воркеров, все `idle`, `Completed = 0` у каждого.

ЧТО НЕ ЗАПРЕЩАЕТСЯ. Диагностика (`status`, `task-status`, `memory`), любые чтения,
и сам `run_task.ps1` — он и есть законный путь. Запрет на исполнение, не на знание.

ТИП — ЗАПРЕТ, не сообщение (Модуль 5 § 10.2): цена пропуска здесь — прогон, который
выглядит роем и им не является, с оплаченными токенами и ложным отчётом. Цена
ложного срабатывания — одна команда, переписанная через обвязку.
"""
from __future__ import annotations

import json
import re
import sys

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

# Запуск роя. Диагностические подкоманды сюда намеренно не входят.
LAUNCH = re.compile(r"hive-mind\s+(spawn|task)\b|autopilot\s+enable\b|swarm\s+start\b", re.I)
# Законный путь: команда идёт через обвязку.
WRAPPER = re.compile(r"run_task\.ps1", re.I)

DENY = (
    "air-ruflo-bridge: прямой запуск роя отклонён (ERR-2026-000192).\n"
    "`hive-mind spawn --claude` поднимает Queen-сессию, но НЕ заводит воркеров и НЕ "
    "кладёт задачу в очередь роя: снаружи похоже на рой, фактически одна дорогая "
    "сессия. Так рой не выполнил НИ ОДНОЙ задачи — 18 воркеров, все idle, Completed=0.\n"
    "Запуск состоит из четырёх шагов, и все четыре зашиты в обвязку:\n"
    "  hive-mind spawn -n <N>  ->  hive-mind task -d  ->  autopilot enable  ->  spawn --claude\n"
    "Законный путь (гейт ЛПР, скан секретов, проверка приёмки по вызовам "
    "mcp__claude-flow__ включены):\n"
    "  skills/run-via-ruflo/scripts/run_task.ps1\n"
    "Диагностика (status, task-status, memory) под запрет не попадает."
)


def read_payload() -> dict:
    """Вход хука. Любой мусор на stdin — не повод уронить чужую сессию."""
    try:
        data = json.loads(sys.stdin.read() or "{}")
    except (ValueError, OSError):
        return {}
    return data if isinstance(data, dict) else {}


def check(command: str) -> str | None:
    if not command or not LAUNCH.search(command) or WRAPPER.search(command):
        return None
    return DENY


def main() -> int:
    payload = read_payload()
    tool_input = payload.get("tool_input")
    tool_input = tool_input if isinstance(tool_input, dict) else {}
    note = check(str(tool_input.get("command") or ""))
    if note:
        print(json.dumps({
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": "deny",
                "permissionDecisionReason": note,
            }
        }, ensure_ascii=False))
    return 0


def _selftest() -> None:
    assert check('node cli.js hive-mind spawn --claude -o "цель"')
    assert check("npx claude-flow autopilot enable")
    assert check("hive-mind task -d 'задача' -p high")
    # Законный путь и диагностика молчат.
    assert check('& "…/scripts/run_task.ps1" -Objective x -TargetPath y') is None
    assert check("node cli.js hive-mind status") is None
    assert check("git status") is None
    assert check("") is None
    # Хук не должен падать ни на каком входе.
    assert main() is not None or True
    print("selftest ok")


if __name__ == "__main__":
    if "--selftest" in sys.argv:
        _selftest()
    else:
        sys.exit(main())
