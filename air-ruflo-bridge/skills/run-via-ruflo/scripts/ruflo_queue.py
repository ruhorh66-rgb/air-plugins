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

import json
import os
import re
import subprocess
import sys
from datetime import datetime, timedelta, timezone

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

# Пути подменяемы через окружение. Не «на всякий случай»: проверять поведение
# очереди можно только на копии страницы и на своём замке — на живой странице
# TASK-OBS-0041 и на замке работающего роя проверка означала бы порчу того самого
# состояния, ради которого она пишется. Значения по умолчанию — боевые.
PLATFORM = os.environ.get("RUFLO_PLATFORM") or r"E:\-5-\010_Task_Control_Platform"
PAGE = os.environ.get("RUFLO_QUEUE_PAGE") or os.path.join(
    PLATFORM, "02_WIKI", "TASK-OBS-0041 — очередь вайб-кодинга для роя.md")
HERE = os.path.dirname(os.path.abspath(__file__))
APPROVE = os.path.join(HERE, "approve_via_telegram.py")
RUN_TASK = os.path.join(HERE, "run_task.ps1")
HIVE_SINGLE = os.path.join(HERE, "hive_single.ps1")
REPORTS = os.environ.get("RUFLO_REPORTS") or r"E:\-4-\ruflo-hive"
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


HIVE_LOCK = os.path.join(REPORTS, "hive.lock.json")


def _lock_state(status: dict | None) -> tuple[str, str]:
    """Вердикт `hive_single.ps1 status` → одно из free | held | unknown.

    ТРИ состояния, а не два. `held` — рой занят живым держателем. `free` — замка
    нет либо он остался от мёртвого процесса (это мусор, а не занятость). `unknown` —
    замок есть, но что он значит, установить не удалось: файл не разобрался, поля pid
    нет, сам опрос не отработал.

    `unknown` НЕ склеивается с `free` намеренно. Прежняя редакция считала нечитаемый
    замок свободой, и тогда поверх работающего роя выдавалась вторая задача — то есть
    единственный источник факта «занят» молча переставал быть источником.
    """
    if not isinstance(status, dict):
        return "unknown", "опросить замок не удалось — это НЕ «свободно»"
    state = str(status.get("lockState") or "")
    who = (f"{status.get('lockOwner')}/{status.get('lockPid')}"
           f" с {status.get('lockSince')}, задача {status.get('taskId') or '—'}")
    if state == "held":
        return "held", f"замок держит {who} ({status.get('livenessProof')})"
    if state == "stale":
        return "free", f"замок остался от мёртвого держателя {who} ({status.get('livenessProof')})"
    if state == "free":
        return "free", "замка нет"
    return "unknown", f"замок в состоянии «{state or 'без ответа'}»: {status.get('livenessProof')}"


def hive_state() -> tuple[str, str, dict]:
    """Занят ли рой ПО ФАКТУ. Возвращает (состояние, чем подтверждено, сам замок).

    ЗАЧЕМ НЕ ВЕРИТЬ СТРОКЕ ОЧЕРЕДИ. `approved` в таблице означает «мы отметили, что
    запускаем», и между отметкой и правдой есть зазор: прогон мог упасть до запуска
    (исключение при вызове PowerShell — строка остаётся approved навсегда), рой мог
    держать посторонняя ручная сессия, и тогда её завершение никакого события в
    очереди не рождает. Оба случая — blocker'ы codex review 09.08.2026, и оба дают
    одно последствие: очередь встаёт молча и навсегда.

    Замок — источник, отметка — производное.

    ЖИВОСТЬ ДЕРЖАТЕЛЯ СЧИТАЕТСЯ НЕ ЗДЕСЬ. Спрашиваем `hive_single.ps1 -Action status`,
    который и пишет замок: правило «PID и время старта процесса» живёт в одном месте.
    Своя вторая реализация той же проверки на Python — это ровно те два источника, от
    которых мы уходим. `-NoScan` потому, что обход дисков в поисках чужих каталогов
    состояния занимает секунды, а замок спрашивается на каждый push.
    """
    try:
        res = subprocess.run(["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass",
                              "-File", HIVE_SINGLE, "-Action", "status",
                              "-Root", REPORTS, "-NoScan"],
                             capture_output=True, text=True, encoding="utf-8",
                             errors="replace", timeout=60)
        status = json.loads(res.stdout)
    except Exception as exc:
        return "unknown", f"опрос замка не отработал ({type(exc).__name__}: {exc})", {}
    state, why = _lock_state(status)
    return state, why, status


def _next_queued(rows: list[dict]) -> dict | None:
    """Первая ждущая по приоритету — и только это.

    ЗАНЯТОСТЬ РОЯ ЗДЕСЬ БОЛЬШЕ НЕ РЕШАЕТСЯ. Раньше замок спрашивался отсюда, и только
    если в таблице нашлась строка `approved`: ручной держатель замка при всех строках
    `queued` не проверялся вовсе, и заявка выдавалась поверх работающего роя. Это и был
    второй источник факта «занят». Теперь занятость решена выше — по замку, всегда, до
    выбора строки (`cmd_push`), а строка `approved` ничего не блокирует.
    """
    waiting = [r for r in rows if r["action"] == "queued"]
    waiting.sort(key=lambda r: {"critical": 0, "high": 1, "normal": 2}.get(r["priority"], 3))
    return waiting[0] if waiting else None


def _stale_marks(rows: list[dict], mine: str) -> list[str]:
    """Отметки `approved`, пережившие свой прогон, — все, кроме строки держателя."""
    if not mine:
        return []
    return [r["task_id"] for r in rows if r["action"] == "approved" and r["task_id"] != mine]


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
    # ЗАМОК СПРАШИВАЕТСЯ ПЕРВЫМ ДЕЛОМ И ВСЕГДА — до выбора строки и независимо от
    # того, есть ли в таблице `approved`. Прежде вопрос задавался условно, и держатель
    # замка, не отражённый в таблице (ручная сессия, прогон вне очереди), не мешал
    # выдать вторую задачу поверх работающего роя.
    state, why, _lock = hive_state()
    if state == "held":
        print(f"рой занят — новая задача не выдаётся: {why}", file=sys.stderr)
        return 4
    if state == "unknown":
        print(f"состояние замка неизвестно, руками разобраться: {why}\n"
              f"замок: {HIVE_LOCK}\n"
              f"это НЕ «свободно» — пока не разобрано, задача не выдаётся", file=sys.stderr)
        return 5

    rows, broken = read_queue()
    if broken:
        print(f"внимание: {len(broken)} строк не разобрано, они пропущены — см. list")
    row = _next_queued(rows) if not argv else next(
        (r for r in rows if r["task_id"] == argv[0]), None)
    if not row:
        # Отметок `approved` при свободном замке быть не должно: они переживут свой
        # прогон и починятся ближайшим ВЗЯТИЕМ замка (`reconcile` из run_task.ps1).
        # Но если ждущих строк нет вовсе, взять замок неоткуда — называем вслух.
        stuck = [r["task_id"] for r in rows if r["action"] == "approved"]
        if stuck:
            print("в очереди нет задач, ждущих запуска; при этом замок свободен, а "
                  f"отметку «approved» несут: {', '.join(stuck)} — они пережили свой "
                  "прогон. Починятся сами при ближайшем прогоне; вернуть сейчас: "
                  f"record {stuck[0]} queued")
            return 3
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
             "-Workers", workers, "-Priority", priority, "-TaskId", row["task_id"]],
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


def cmd_reconcile(argv: list[str]) -> int:
    """Вернуть в `queued` отметки, пережившие свой прогон. Право — у держателя замка.

    ПОЧЕМУ НЕ «У ЛЮБОГО ПОСМОТРЕВШЕГО». Снять отметку — значит объявить, что прогон по
    ней не идёт. Такое знание есть ровно у одного: у того, кто ТОЛЬКО ЧТО взял замок, —
    рой в этот момент доказанно свободен для всех остальных строк. Любой другой,
    увидевший свободный замок, мог смотреть в зазор между взятием замка и записью
    строки — и снял бы отметку с прогона, который в этот момент начинается.

    Вызывается из `run_task.ps1` сразу после успешного claim, с собственным pid. Тем
    самым падение между взятием замка и записью строки чинится СЛЕДУЮЩИМ взятием, а не
    сторожем и не человеком.
    """
    if not argv:
        print("usage: reconcile <pid процесса, который держит замок>", file=sys.stderr)
        return 2
    lock = {}
    try:
        with open(HIVE_LOCK, encoding="utf-8") as fh:
            lock = json.load(fh)
    except (OSError, ValueError):
        print("замка нет либо он нечитаем — чинить строки некому", file=sys.stderr)
        return 4
    if str(lock.get("pid")) != str(argv[0]).strip():
        print(f"замок держит pid {lock.get('pid')}, а вызвал {argv[0]} — не твоё дело",
              file=sys.stderr)
        return 4
    mine = str(lock.get("taskId") or "")
    if not mine:
        # Замок без идентификатора задачи (ручной прогон): какая строка «своя» —
        # неизвестно, а снимать отметки наугад значило бы сбить чужой живой прогон.
        print("в замке нет taskId — строки не трогаю")
        return 0
    rows, _ = read_queue()
    fixed = _stale_marks(rows, mine)
    for task_id in fixed:
        _write_state(task_id, "queued")
        _append_journal(task_id, "queued",
                        f"отметка пережила свой прогон; снята при взятии замка "
                        f"pid {lock.get('pid')} под {mine}")
        print(f"{task_id}: отметка «approved» снята, строка возвращена в очередь")
    if not fixed:
        print(f"переживших отметок нет (замок держит {mine})")
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
    # Выдуманные строки: настоящую страницу не трогаем, замок не спрашиваем.
    assert _next_queued(sample)["task_id"] == "TASK-OBS-3", "приоритет решает порядок"
    busy = sample + [{"task_id": "TASK-OBS-4", "action": "approved", "priority": "high"}]
    # Отметка `approved` больше НЕ решает вопрос занятости — его решает замок, выше и
    # всегда. Пережившая отметка поэтому не запирает очередь (blocker'ы codex review
    # 09.08.2026), а второй задачи поверх работающего роя не даёт замок.
    assert _next_queued(busy)["task_id"] == "TASK-OBS-3", \
        "отметка approved не должна запирать очередь — занятость решает замок"
    unknown = [{"task_id": "TASK-OBS-9", "action": "непонятно", "priority": "high"}]
    assert _next_queued(unknown) is None, "неизвестное состояние в работу не берётся"
    assert _next_queued([r for r in busy if r["action"] != "queued"]) is None, \
        "ждущих нет — выдавать нечего"

    # Занятость. Проверка та же по строгости, что была у отбора: занят — не выдаём.
    # Плюс то, чего прежняя схема не умела вовсе: непрочитанный замок ≠ свободный.
    assert _lock_state({"lockState": "held"})[0] == "held", "живой держатель — занято"
    assert _lock_state({"lockState": "free"})[0] == "free"
    assert _lock_state({"lockState": "stale"})[0] == "free", "мёртвый держатель — мусор"
    assert _lock_state({"lockState": "unknown"})[0] == "unknown"
    assert _lock_state({"lockState": ""})[0] == "unknown", "без ответа — не «свободно»"
    assert _lock_state(None)[0] == "unknown", "опрос не отработал — не «свободно»"

    # Право чинить строку есть только у держателя, и только на ЧУЖИЕ строки.
    assert _stale_marks(busy, "TASK-OBS-4") == [], "свою строку держатель не снимает"
    assert _stale_marks(busy, "TASK-OBS-3") == ["TASK-OBS-4"], "чужая пережившая — снимается"
    assert _stale_marks(busy, "") == [], "замок без taskId — не трогаем ничего"
    print(f"selftest ok: строк в очереди {len(rows)}, испорченных нет")
    return 0


COMMANDS = {"list": cmd_list, "push": cmd_push, "record": cmd_record,
            "reconcile": cmd_reconcile, "--selftest": lambda _a: _selftest()}


def main(argv: list[str]) -> int:
    if not argv or argv[0] not in COMMANDS:
        print(__doc__)
        print("команды: " + ", ".join(sorted(COMMANDS)))
        print(f"очередь: {PAGE}")
        return 2
    return COMMANDS[argv[0]](argv[1:])


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
