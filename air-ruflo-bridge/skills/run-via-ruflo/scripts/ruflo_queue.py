r"""Очередь задач на вайб-кодинг для роя — одна страница на весь контур.

ЗАЧЕМ. Механизм запуска роя был собран (четыре шага, гейт ЛПР, скан секретов,
приёмка по вызовам mcp__claude-flow__), но источника задач у него не было: цель
собиралась руками в отдельный файл, и очередь существовала только в голове той
сессии, которая рой запускала.

ПОЧЕМУ СТРАНИЦА-ЗАДАЧА, А НЕ РЕЕСТР CSV. Первая редакция завела
`00_REGISTRY/ruflo_queue.csv`, и ЛПР справедливо сказал, что это не то: CSV удобен
скрипту, но задачей не является — в списке задач не виден, в Обсидиане не
открывается, и соседняя сессия про него сама не узнает. Писать же в очередь будут
не скрипты, а сессии и человек. Поэтому очередь живёт там, где живут задачи:
`TASK-OBS-0041`, обычная страница, которую видно наравне с остальными.

Двух источников не заводим намеренно. Страница плюс CSV на один смысл разошлись бы
через неделю, и никто бы не знал, какой из них правильный, — этот класс контур уже
видел на плагинах (ERR-2026-000203).

ЧТО ТЕРЯЕТСЯ И ЧЕМ ЗАКРЫТО. Markdown-таблица допускает опечатку, которую CSV бы
отверг. Поэтому строка, которая не разобралась, НАЗЫВАЕТСЯ вслух и не берётся в
работу — а не пропускается молча. Неизвестное состояние тоже не берётся: счесть
незнакомое готовым — тот же отказ, что счесть непроверенное чистым.

ЧЕГО ЭТОТ СКРИПТ НЕ ДЕЛАЕТ. Он не запускает рой. Он готовит заявку и отдаёт её
человеку кнопкой в Telegram; запуск делает слушатель после нажатия. Гейт снять
нельзя и не нужно: классификатор Claude Code блокирует автономный запуск как класс
действия, а цена гейта теперь — одна кнопка, а не поход к консоли.
"""
from __future__ import annotations

import os
import re
import subprocess
import sys
from datetime import datetime, timedelta, timezone

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

PLATFORM = r"E:\-5-\010_Task_Control_Platform"
PAGE = os.path.join(PLATFORM, "02_WIKI", "TASK-OBS-0041 — очередь вайб-кодинга для роя.md")
HERE = os.path.dirname(os.path.abspath(__file__))
APPROVE = os.path.join(HERE, "approve_via_telegram.py")
RUN_TASK = os.path.join(HERE, "run_task.ps1")
REPORTS = r"E:\-4-\ruflo-hive"
# Путь к движку — обязательный параметр run_task.ps1. Держим тот же, что у слушателя
# (approve_listener.CLI_PATH): два разных пути означали бы два разных движка, и
# dry-run проверял бы не то, что потом исполнится.
CLI_PATH = (r"E:\-4-\ruflo-pilot\.npm-cache-3.34.0\_npx\2ed56890c96f58f7"
            r"\node_modules\@claude-flow\cli\bin\cli.js")
OMSK = timezone(timedelta(hours=6))  # контур живёт по Омску, машина — по Pacific

COLUMNS = ["task_id", "action", "objective", "work_dir", "workers", "priority", "by"]
KNOWN = {"queued", "approved", "done", "failed", "cancelled"}
CLOSED = {"done", "failed", "cancelled"}
TASK_RE = re.compile(r"^TASK-[A-Z]+-\d+$")


def _cells(line: str) -> list[str]:
    return [c.strip() for c in line.strip().strip("|").split("|")]


def read_queue() -> tuple[list[dict], list[str]]:
    """(задачи, испорченные строки). Вторая половина возвращается, а не глотается."""
    with open(PAGE, encoding="utf-8") as fh:
        lines = fh.read().splitlines()

    rows, broken, inside = [], [], False
    for line in lines:
        stripped = line.strip()
        if stripped.startswith("| задача |"):
            inside = True
            continue
        if inside:
            if not stripped.startswith("|"):
                break  # таблица кончилась
            cells = _cells(stripped)
            if set("".join(cells)) <= set("-: "):
                continue  # разделитель шапки
            if not TASK_RE.match(cells[0]):
                broken.append(stripped[:110])
                continue
            if len(cells) < len(COLUMNS):
                broken.append(stripped[:110])
                continue
            rows.append(dict(zip(COLUMNS, cells)))
    return rows, broken


def _write_state(task_id: str, action: str) -> None:
    """Поменять состояние ОДНОЙ строки очереди — единственная правка на месте.

    Она допустима именно потому, что правится собственное поле состояния собственной
    строки, найденной по идентификатору И по текущему состоянию. Исход при этом всё
    равно ДОПИСЫВАЕТСЯ в журнал отдельной строкой: история не переписывается.
    """
    with open(PAGE, encoding="utf-8") as fh:
        lines = fh.read().splitlines(keepends=True)
    hit = 0
    for index, line in enumerate(lines):
        if not line.strip().startswith("| " + task_id + " "):
            continue
        cells = _cells(line)
        if len(cells) < len(COLUMNS) or cells[1] not in KNOWN:
            continue
        cells[1] = action
        lines[index] = "| " + " | ".join(cells) + " |\n"
        hit += 1
    if hit != 1:
        raise SystemExit(f"{task_id}: строк для правки {hit}, ожидалась ровно одна")
    with open(PAGE, "w", encoding="utf-8") as fh:
        fh.writelines(lines)


def _append_journal(task_id: str, outcome: str, proof: str) -> None:
    with open(PAGE, encoding="utf-8") as fh:
        text = fh.read()
    head = "| когда | задача | исход | чем подтверждено |\n|---|---|---|---|\n"
    if head not in text:
        raise SystemExit("в странице нет журнала исходов — не дописываю вслепую")
    stamp = datetime.now(OMSK).isoformat(timespec="seconds")
    row = f"| {stamp} | {task_id} | {outcome} | {proof} |\n"
    with open(PAGE, "w", encoding="utf-8") as fh:
        fh.write(text.replace(head, head + row, 1))


def cmd_list(_argv: list[str]) -> int:
    rows, broken = read_queue()
    for row in rows:
        action = row["action"]
        mark = "" if action in KNOWN else "  <- состояние неизвестно, в работу не берётся"
        print(f"  {row['task_id']:<16}{action:<10}{row['objective'][:58]}{mark}")
    if broken:
        print("\nНЕ РАЗОБРАНЫ (в работу не берутся):")
        for line in broken:
            print("  ", line)
    waiting = sum(1 for r in rows if r["action"] == "queued")
    running = sum(1 for r in rows if r["action"] == "approved")
    print(f"\nждёт запуска: {waiting} | в работе: {running}"
          + (f" | испорченных строк: {len(broken)}" if broken else ""))
    return 0


def _next_queued(rows: list[dict]) -> dict | None:
    """Первая ждущая. Пока одна в работе — не выдаём вторую: рой в контуре один."""
    if any(r["action"] == "approved" for r in rows):
        return None
    waiting = [r for r in rows if r["action"] == "queued"]
    waiting.sort(key=lambda r: {"critical": 0, "high": 1, "normal": 2}.get(r["priority"], 3))
    return waiting[0] if waiting else None


def _resolve(path_or_link: str) -> str:
    """Ссылка [[...]] либо путь от корня платформы — в абсолютный путь файла."""
    name = path_or_link.strip()
    link = re.match(r"^\[\[(.+?)\]\]$", name)
    if link:
        name = link.group(1).split("|")[0].strip()
        for root, _dirs, files in os.walk(os.path.join(PLATFORM, "02_WIKI")):
            for candidate in files:
                if candidate == name or candidate == name + ".md":
                    return os.path.join(root, candidate)
        raise SystemExit(f"по ссылке [[{name}]] страница не найдена")
    # Абсолютный путь тоже принимается: задание не обязано лежать в платформе 010.
    # Заявки на доработку плагинов живут в самих репозиториях плагинов рядом с кодом,
    # и заводить в 010 страницу-обёртку ради ссылки — это дубль, который завтра
    # разойдётся с оригиналом.
    if os.path.isabs(name):
        if not os.path.isfile(name):
            raise SystemExit(f"нет файла задания: {name}")
        return name
    full = os.path.join(PLATFORM, name)
    if not os.path.isfile(full):
        raise SystemExit(f"нет файла задания: {name}")
    return full


def cmd_push(argv: list[str]) -> int:
    """Взять следующую: dry-run, затем кнопка ЛПР. Порядок не сокращается."""
    rows, broken = read_queue()
    if broken:
        print(f"внимание: {len(broken)} строк не разобрано, они пропущены — см. list")
    row = _next_queued(rows) if not argv else next(
        (r for r in rows if r["task_id"] == argv[0]), None)
    if not row:
        if any(r["action"] == "approved" for r in rows):
            print("рой уже занят задачей из очереди — новая не выдаётся")
            return 4
        print("в очереди нет задач, ждущих запуска")
        return 3
    if row["action"] != "queued":
        print(f"{row['task_id']}: состояние «{row['action']}», не ждёт запуска")
        return 3

    objective = _resolve(row["objective"])
    work_dir = row["work_dir"]
    os.makedirs(work_dir, exist_ok=True)  # рой работает В каталоге, создать его — наше дело
    report = os.path.join(REPORTS, f"{row['task_id'].lower()}-dryrun.md")
    workers = row["workers"] if str(row["workers"]).isdigit() else "5"
    # `critical` НЕ подаётся движку, хотя он его принимает как значение.
    # Установлено 08.08.2026 сравнением двух прогонов: с `-p high` второй шаг проходит
    # и печатает «Assigned: pending dispatch»; с `-p critical` тот же шаг падает —
    # `result.assignedTo` не определён, и движок роняет печать результата. Разница
    # между прогонами была только в приоритете.
    #
    # Чиним СВОЮ сторону, а не движок (решение ЛПР): порядок в очереди мы задаём сами,
    # а движку нужен лишь работающий приоритет. `critical` остаётся в очереди как
    # признак срочности — он и решает, что выдать первой.
    priority = row["priority"] if row["priority"] in ("normal", "high") else "high"

    # ВЫВОД В ФАЙЛЫ, А НЕ В КАНАЛЫ — иначе dry-run висит вечно.
    # Первый запуск это и показал: `hive-mind spawn` оставляет после себя живого
    # демона (node, claude-flow), тот наследует наши stdout/stderr, и capture_output
    # ждёт EOF, которого не будет, пока демон жив. Процесс висел без единого
    # дочернего — снаружи выглядело как зависший python, хотя PowerShell давно
    # отработал. Файлы этой связи не создают.
    out_path = os.path.join(REPORTS, f"{row['task_id'].lower()}-dryrun.out")
    err_path = os.path.join(REPORTS, f"{row['task_id'].lower()}-dryrun.err")
    os.makedirs(REPORTS, exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as fout, \
            open(err_path, "w", encoding="utf-8") as ferr:
        code = subprocess.call(
            ["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", RUN_TASK,
             "-Objective", open(objective, encoding="utf-8").read(),
             "-TargetPath", work_dir, "-ReportPath", report, "-CliPath", CLI_PATH,
             "-Workers", workers, "-Priority", priority],
            stdout=fout, stderr=ferr, stdin=subprocess.DEVNULL, timeout=900)

    class _Result:  # тот же интерфейс, что был у CompletedProcess
        returncode = code
        stdout = open(out_path, encoding="utf-8", errors="replace").read()
        stderr = open(err_path, encoding="utf-8", errors="replace").read()

    dry = _Result()
    # 10 — «предложение готово, человек не подтверждал»: ШТАТНЫЙ исход dry-run,
    # обвязка намеренно останавливается до запуска.
    if dry.returncode not in (10, 0):
        # Печатаем ОБА потока: отказ по параметрам PowerShell пишет только в stderr,
        # и первая редакция показала голое «код 1» без причины — пришлось выяснять
        # отдельным прогоном то, что уже было известно скрипту.
        print(f"dry-run не прошёл, код {dry.returncode}", file=sys.stderr)
        for name, stream in (("stdout", dry.stdout), ("stderr", dry.stderr)):
            text = (stream or "").strip()
            if text:
                print(f"--- {name} ---\n{text[-900:]}", file=sys.stderr)
        return dry.returncode

    sent = subprocess.run(
        [sys.executable, APPROVE, "create", report, objective, work_dir, workers, priority,
         f"{row['task_id']} (очередь роя)"],
        capture_output=True, text=True, encoding="utf-8", errors="replace")
    print(sent.stdout.strip() or sent.stderr.strip())
    return 0 if sent.returncode == 0 else sent.returncode


def cmd_record(argv: list[str]) -> int:
    """Записать исход: состояние строки + новая строка журнала."""
    if len(argv) < 2:
        print("usage: record <TASK-ID> <approved|done|failed|cancelled> [чем подтверждено]",
              file=sys.stderr)
        return 2
    task_id, action = argv[0], argv[1]
    if action not in KNOWN:
        raise SystemExit(f"неизвестное состояние: {action}; допустимы {sorted(KNOWN)}")
    _write_state(task_id, action)
    _append_journal(task_id, action, argv[2] if len(argv) > 2 else "—")
    print(f"{task_id}: {action}")
    if action in CLOSED:
        rows, _ = read_queue()
        nxt = _next_queued(rows)
        if nxt:
            print(f"следующая в очереди: {nxt['task_id']}")
    return 0


def _selftest() -> int:
    """Проверка того, что решает: разбор таблицы, отбор, испорченные строки."""
    rows, broken = read_queue()
    assert rows, "таблица очереди не разобралась вовсе"
    assert all(TASK_RE.match(r["task_id"]) for r in rows)
    assert not broken, f"в живой странице испорченные строки: {broken}"

    sample = [
        {"task_id": "TASK-OBS-1", "action": "done", "priority": "high"},
        {"task_id": "TASK-OBS-2", "action": "queued", "priority": "normal"},
        {"task_id": "TASK-OBS-3", "action": "queued", "priority": "critical"},
    ]
    assert _next_queued(sample)["task_id"] == "TASK-OBS-3", "приоритет решает порядок"
    busy = sample + [{"task_id": "TASK-OBS-4", "action": "approved", "priority": "high"}]
    assert _next_queued(busy) is None, "пока одна в работе, вторая не выдаётся"
    unknown = [{"task_id": "TASK-OBS-9", "action": "непонятно", "priority": "high"}]
    assert _next_queued(unknown) is None, "неизвестное состояние в работу не берётся"
    print(f"selftest ok: строк в очереди {len(rows)}, испорченных нет")
    return 0


COMMANDS = {"list": cmd_list, "push": cmd_push, "record": cmd_record,
            "--selftest": lambda _a: _selftest()}


def main(argv: list[str]) -> int:
    if not argv or argv[0] not in COMMANDS:
        print(__doc__)
        print("команды: " + ", ".join(sorted(COMMANDS)))
        print(f"очередь: {PAGE}")
        return 2
    return COMMANDS[argv[0]](argv[1:])


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
