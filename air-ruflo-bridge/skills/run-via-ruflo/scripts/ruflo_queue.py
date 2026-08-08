r"""Очередь задач на вайб-кодинг для роя — одна на весь контур.

ЗАЧЕМ. Механизм запуска роя собран (заявка -> кнопка в Telegram -> четыре шага
запуска -> скан секретов -> приёмка по вызовам mcp__claude-flow__), но источника
задач у него не было: цель собиралась руками, а очередь жила в голове сессии.
Решение ЛПР 08.08.2026: одна задача на рой, куда пишут все сессии.

ПОЧЕМУ ОДИН ФАЙЛ-ЖУРНАЛ, А НЕ ТАБЛИЦА СО СТАТУСАМИ. Правка существующей строки в
общем реестре — то, на чём контур уже обжёгся: массовая правка по списку ID
переписала записи параллельной сессии (ERR-2026-000199). Здесь каждое событие —
НОВАЯ строка, и текущее состояние задачи есть её последнее событие. Дописывание
двух сессий одновременно не портит чужого: худшее, что может случиться, — порядок
строк, а он и так восстанавливается по метке времени.

СОСТОЯНИЯ: queued -> approved -> done | failed | cancelled. Любое иное значение
считается неизвестным и в работу не берётся: молча трактовать незнакомое как
готовое — тот же класс, что «непроверенное считать чистым».

ЧЕГО ЭТОТ СКРИПТ НЕ ДЕЛАЕТ. Он не запускает рой. Он готовит заявку и отдаёт её
человеку кнопкой; запуск делает слушатель после нажатия. Гейт человека снять нельзя
и не нужно: классификатор Claude Code блокирует автономный запуск как класс
действия, а цена гейта теперь — одна кнопка, а не поход к консоли.
"""
from __future__ import annotations

import csv
import os
import subprocess
import sys
from datetime import datetime, timedelta, timezone

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

PLATFORM = r"E:\-5-\010_Task_Control_Platform"
QUEUE = os.path.join(PLATFORM, "00_REGISTRY", "ruflo_queue.csv")
HERE = os.path.dirname(os.path.abspath(__file__))
APPROVE = os.path.join(HERE, "approve_via_telegram.py")
RUN_TASK = os.path.join(HERE, "run_task.ps1")
REPORTS = r"E:\-4-\ruflo-hive"
OMSK = timezone(timedelta(hours=6))  # контур живёт по Омску, машина — по Pacific

FIELDS = ["ts", "task_id", "action", "title", "objective_path", "work_dir",
          "workers", "priority", "requested_by", "note"]
KNOWN = {"queued", "approved", "done", "failed", "cancelled"}
CLOSED = {"done", "failed", "cancelled"}


def _rows() -> list[dict]:
    try:
        with open(QUEUE, encoding="utf-8", newline="") as fh:
            return [r for r in csv.DictReader(fh) if r.get("task_id")]
    except OSError:
        return []


def _append(row: dict) -> None:
    exists = os.path.isfile(QUEUE)
    os.makedirs(os.path.dirname(QUEUE), exist_ok=True)
    with open(QUEUE, "a", encoding="utf-8", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=FIELDS)
        if not exists:
            writer.writeheader()
        writer.writerow({k: row.get(k, "") for k in FIELDS})


def state() -> dict[str, dict]:
    """Текущее состояние каждой задачи — её ПОСЛЕДНЕЕ событие по метке времени."""
    latest: dict[str, dict] = {}
    for row in _rows():
        key = row["task_id"]
        if key not in latest or row.get("ts", "") >= latest[key].get("ts", ""):
            latest[key] = row
    return latest


def cmd_list(_argv: list[str]) -> int:
    current = state()
    if not current:
        print("очередь пуста")
        return 0
    order = {"queued": 0, "approved": 1, "failed": 2, "done": 3, "cancelled": 4}
    for row in sorted(current.values(),
                      key=lambda r: (order.get(r.get("action"), 9), r.get("ts", ""))):
        action = row.get("action", "?")
        mark = "" if action in KNOWN else "  <- состояние неизвестно, в работу не берётся"
        print(f"  {row['task_id']:<16}{action:<10}{(row.get('title') or '')[:60]}{mark}")
    waiting = [r for r in current.values() if r.get("action") == "queued"]
    running = [r for r in current.values() if r.get("action") == "approved"]
    print(f"\nждёт запуска: {len(waiting)} | в работе: {len(running)}")
    return 0


def _next_queued() -> dict | None:
    """Первая задача, ждущая запуска. Если одна уже в работе — не берём вторую.

    Рой в контуре один: замок канонического корня всё равно отклонит второй запуск,
    но лучше не создавать заявку, которая заведомо упрётся в замок.
    """
    current = state()
    if any(r.get("action") == "approved" for r in current.values()):
        return None
    waiting = [r for r in current.values() if r.get("action") == "queued"]
    waiting.sort(key=lambda r: ({"critical": 0, "high": 1, "normal": 2}.get(r.get("priority"), 3),
                                r.get("ts", "")))
    return waiting[0] if waiting else None


def cmd_add(argv: list[str]) -> int:
    """Поставить задачу в очередь. Пишет строку, ничего не запускает."""
    if len(argv) < 3:
        print("usage: add <TASK-ID> <путь к заданию от корня платформы> <рабочий каталог> "
              "[воркеров] [приоритет] [заголовок]", file=sys.stderr)
        return 2
    task_id, objective, work_dir = argv[0], argv[1], argv[2]
    if not os.path.isfile(os.path.join(PLATFORM, objective)):
        raise SystemExit(f"нет файла задания: {objective}")
    _append({
        "ts": datetime.now(OMSK).isoformat(timespec="seconds"),
        "task_id": task_id, "action": "queued",
        "title": argv[5] if len(argv) > 5 else task_id,
        "objective_path": objective, "work_dir": work_dir,
        "workers": argv[3] if len(argv) > 3 else "5",
        "priority": argv[4] if len(argv) > 4 else "high",
        "requested_by": os.environ.get("AIR_SESSION", "сессия"),
        "note": "",
    })
    print(f"поставлено в очередь: {task_id}")
    return 0


def cmd_push(argv: list[str]) -> int:
    """Взять следующую задачу: dry-run и кнопка ЛПР в Telegram.

    Порядок именно такой и не сокращается: dry-run показывает, что именно уйдёт рою,
    и только после него человек получает кнопку. Заявка хранит ПАРАМЕТРЫ, а не готовую
    команду — иначе канал подтверждения превратился бы в удалённое исполнение кода.
    """
    row = _next_queued() if not argv else state().get(argv[0])
    if not row:
        current = state()
        if any(r.get("action") == "approved" for r in current.values()):
            print("рой уже занят задачей из очереди — новая не выдаётся")
            return 4
        print("в очереди нет задач, ждущих запуска")
        return 3
    if row.get("action") != "queued":
        print(f"{row['task_id']}: состояние «{row.get('action')}», не ждёт запуска")
        return 3

    objective = os.path.join(PLATFORM, row["objective_path"])
    if not os.path.isfile(objective):
        raise SystemExit(f"задание исчезло: {objective}")
    work_dir = row["work_dir"]
    os.makedirs(work_dir, exist_ok=True)  # рой работает В каталоге, создать его — наше дело
    report = os.path.join(REPORTS, f"{row['task_id'].lower()}-dryrun.md")

    dry = subprocess.run(
        ["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", RUN_TASK,
         "-Objective", open(objective, encoding="utf-8").read(),
         "-TargetPath", work_dir, "-ReportPath", report,
         "-Workers", str(row.get("workers") or 5),
         "-Priority", str(row.get("priority") or "high")],
        capture_output=True, text=True, encoding="utf-8", errors="replace")
    # 10 — «предложение готово, человек не подтверждал». Это ШТАТНЫЙ исход dry-run,
    # а не отказ: обвязка намеренно останавливается до запуска.
    if dry.returncode not in (10, 0):
        print(f"dry-run не прошёл, код {dry.returncode}", file=sys.stderr)
        print((dry.stdout or "")[-800:], file=sys.stderr)
        return dry.returncode

    sent = subprocess.run(
        [sys.executable, APPROVE, "create", report, objective, work_dir,
         str(row.get("workers") or 5), str(row.get("priority") or "high"),
         f"{row['task_id']}: {row.get('title') or ''}"],
        capture_output=True, text=True, encoding="utf-8", errors="replace")
    print(sent.stdout.strip() or sent.stderr.strip())
    return 0 if sent.returncode == 0 else sent.returncode


def cmd_record(argv: list[str]) -> int:
    """Записать исход. Новой строкой — существующие не правятся никогда."""
    if len(argv) < 2:
        print("usage: record <TASK-ID> <approved|done|failed|cancelled> [заметка]",
              file=sys.stderr)
        return 2
    task_id, action = argv[0], argv[1]
    if action not in KNOWN:
        raise SystemExit(f"неизвестное состояние: {action}; допустимы {sorted(KNOWN)}")
    known = state().get(task_id)
    if not known:
        raise SystemExit(f"{task_id} в очереди не значится")
    _append({
        "ts": datetime.now(OMSK).isoformat(timespec="seconds"),
        "task_id": task_id, "action": action,
        "title": known.get("title", ""), "objective_path": known.get("objective_path", ""),
        "work_dir": known.get("work_dir", ""), "workers": known.get("workers", ""),
        "priority": known.get("priority", ""),
        "requested_by": os.environ.get("AIR_SESSION", "сессия"),
        "note": argv[2] if len(argv) > 2 else "",
    })
    print(f"{task_id}: {action}")
    # Завершилась — сразу предлагаем следующую (решение ЛПР: очередь двигается сама).
    if action in CLOSED:
        nxt = _next_queued()
        if nxt:
            print(f"следующая в очереди: {nxt['task_id']} — {nxt.get('title', '')[:60]}")
    return 0


def _selftest() -> int:
    """Проверка того, что решает: последнее событие определяет состояние."""
    global _rows
    real = _rows
    _rows = lambda: [  # noqa: E731
        {"ts": "2026-08-08T10:00:00+06:00", "task_id": "A", "action": "queued",
         "priority": "high", "title": "первая"},
        {"ts": "2026-08-08T11:00:00+06:00", "task_id": "A", "action": "done", "priority": "high"},
        {"ts": "2026-08-08T09:00:00+06:00", "task_id": "B", "action": "queued",
         "priority": "critical", "title": "вторая"},
    ]
    assert state()["A"]["action"] == "done", "состояние — последнее событие, не первое"
    assert _next_queued()["task_id"] == "B", "закрытая задача не выдаётся, берётся ждущая"

    _rows = lambda: [  # noqa: E731
        {"ts": "1", "task_id": "A", "action": "approved", "priority": "high"},
        {"ts": "2", "task_id": "B", "action": "queued", "priority": "critical"},
    ]
    assert _next_queued() is None, "пока одна в работе, вторая не выдаётся"

    _rows = lambda: [{"ts": "1", "task_id": "A", "action": "непонятно"}]  # noqa: E731
    assert _next_queued() is None, "неизвестное состояние в работу не берётся"

    _rows = real
    print("selftest ok")
    return 0


COMMANDS = {"list": cmd_list, "add": cmd_add, "push": cmd_push, "record": cmd_record,
            "--selftest": lambda _a: _selftest()}


def main(argv: list[str]) -> int:
    if not argv or argv[0] not in COMMANDS:
        print(__doc__)
        print("команды: " + ", ".join(sorted(COMMANDS)))
        return 2
    return COMMANDS[argv[0]](argv[1:])


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
