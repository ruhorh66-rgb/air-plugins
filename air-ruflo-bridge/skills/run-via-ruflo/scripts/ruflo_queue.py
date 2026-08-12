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
import tempfile
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
CLI_PATH = (r"E:\-4-\ruflo-pilot\.npm-cache-3.36.0\_npx\b05ba791a3cfd7b6"
            r"\node_modules\@claude-flow\cli\bin\cli.js")
OMSK = timezone(timedelta(hours=6))  # контур живёт по Омску, машина — по Pacific
# ПАУЗА ОЧЕРЕДИ. Файл есть — выдача следующей задачи не делается, его содержимое
# называется причиной. Ставится на время разбора самого механизма: идущий прогон
# доводится до конца, а очередь за ним не едет дальше.
#
# ponytail: заплата, а не штатный механизм — ставится и снимается правкой файла, в
# `list` не видна, в журнал не пишется. Довести до штатного вида: docs/GOAL.md § 5
# (указание ЛПР 11.08.2026).
#
# Почему файл, а не состояние строк. Пауза относится к ОЧЕРЕДИ, а не к задаче:
# переводить строки в `cancelled` ради остановки значило бы врать об их судьбе, и
# после снятия паузы пришлось бы вспоминать, какие из них были живыми.
PAUSE_FILE = os.environ.get("RUFLO_QUEUE_PAUSE") or os.path.join(
    r"E:\-4-\skill-state\ruflo-approvals", "_paused.txt")

COLUMNS = ["task_id", "action", "objective", "work_dir", "workers", "priority", "by"]
# `offered` — заявка на эту строку УЖЕ создана и ждёт кнопки. Состояние заведено,
# потому что два подряд `push` при свободном рое делали ДВЕ кнопки на одну задачу, и
# приняв обе, задачу можно было запустить дважды — исходный ERR-2026-000216 через
# другую дверь. Это НЕ второй реестр состояния: то же поле той же строки.
KNOWN = {"queued", "offered", "approved", "done", "failed", "cancelled"}
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
        mark = ("  <- заявка создана, ждёт кнопки" if action == "offered"
                else "" if action in KNOWN
                else "  <- состояние неизвестно, в работу не берётся")
        print(f"  {row['task_id']:<16}{action:<10}{row['objective'][:58]}{mark}")
    if broken:
        print("\nНЕ РАЗОБРАНЫ (в работу не берутся):")
        for line in broken:
            print("  ", line)
    waiting = sum(1 for r in rows if r["action"] == "queued")
    running = sum(1 for r in rows if r["action"] == "approved")
    offered = sum(1 for r in rows if r["action"] == "offered")
    print(f"\nждёт запуска: {waiting} | ждёт кнопки: {offered} | в работе: {running}"
          + (f" | испорченных строк: {len(broken)}" if broken else ""))
    return 0


HIVE_LOCK = os.path.join(REPORTS, "hive.lock.json")


def _hive(action: str, extra: list[str] | None = None) -> dict | None:
    """Вызвать `hive_single.ps1` и вернуть его JSON; None — вызов не отработал.

    ДЕРЖАТЕЛЕМ РЕГИСТРИРУЕТСЯ НАШ процесс (`-HolderPid`), а не powershell. Иначе
    «мы держим замок» — самообман: powershell завершается сразу после claim, его PID
    становится мёртвым, замок — протухшим, и следующий претендент законно его
    перехватит, пока мы всё ещё считаем рой своим.
    """
    try:
        res = subprocess.run(["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass",
                              "-File", HIVE_SINGLE, "-Action", action, "-Root", REPORTS,
                              "-HolderPid", str(os.getpid())] + (extra or []),
                             capture_output=True, text=True, encoding="utf-8",
                             errors="replace", timeout=60)
        return json.loads(res.stdout)
    except Exception:
        return None


def _claim_state(result: dict | None) -> tuple[str, str]:
    """Ответ `claim` → одно из claimed | busy | unknown.

    ТРИ состояния, а не два, и `unknown` НЕ склеивается со «свободно» намеренно:
    именно эта склейка выдавала вторую задачу поверх работающего роя. Протухший
    замок отдельным состоянием больше не является — его перехватывает сам `claim`,
    и наружу это выходит как обычное `claimed`.
    """
    if not isinstance(result, dict):
        return "unknown", "взять замок не удалось, ответа нет — это НЕ «свободно»"
    outcome = str(result.get("outcome") or "")
    if outcome == "claimed":
        return "claimed", ("замок взят" + (" (перехвачен протухший: "
                           f"{result.get('replacedProof')})" if result.get("replacedStaleLock")
                           else ""))
    if outcome == "busy":
        who = (f"{result.get('heldBy')}/{result.get('pid')} с {result.get('since')}, "
               f"задача {result.get('taskId') or '—'}")
        return "busy", str(result.get("reason") or
                           f"замок держит {who} ({result.get('livenessProof')})")
    return "unknown", (f"замок в состоянии «{outcome or 'без ответа'}»: "
                       f"{result.get('reason') or result.get('advice') or '—'}")


def hive_claim(task_id: str = "") -> tuple[str, str]:
    """ВЗЯТЬ рой под себя. Возвращает (claimed | busy | unknown, чем подтверждено).

    ПОЧЕМУ БЕРЁМ, А НЕ СПРАШИВАЕМ. Спросить `status` — значит узнать, что было
    мгновение назад; между ответом и нашим действием рой успевает занять кто угодно.
    Право действовать даёт владение, а не наблюдение: `claim` создаёт замок ОДНОЙ
    операцией ядра, и победитель ровно один. Всё, что мы делаем со строкой таблицы
    дальше, делается под этим владением — потому две одновременные выдачи и
    невозможны, а не «маловероятны».

    ЖИВОСТЬ ДЕРЖАТЕЛЯ СЧИТАЕТСЯ НЕ ЗДЕСЬ: правило «PID и время старта процесса»
    живёт в `hive_single.ps1`, который замок и пишет. Своя вторая реализация той же
    проверки на Python — это ровно те два источника, от которых мы уходим.
    """
    extra = ["-Owner", f"queue-push-{os.getpid()}"]
    if task_id:
        extra += ["-TaskId", task_id]
    return _claim_state(_hive("claim", extra))


def hive_release() -> None:
    """Отпустить свой замок. Чужой не снимается: -Force здесь нет и не будет."""
    _hive("release")


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
    """Отметки `approved`, пережившие свой прогон, — все, кроме строки держателя.

    `offered` сюда НЕ входит: это не переживший прогон, а живая заявка, по которой
    кнопка ещё висит. Снять её пришлось бы вслепую, а закрывает её тот, кто о решении
    человека знает, — слушатель.
    """
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


def _reserve(task_id: str) -> None:
    """Строка переходит queued → offered. Вызывается ТОЛЬКО при взятом замке."""
    _write_state(task_id, "offered")
    _append_journal(task_id, "offered",
                    "строка зарезервирована под взятым замком, заявка готовится")


def _unreserve(task_id: str, why: str) -> None:
    """Вернуть зарезервированную строку в очередь: заявки не будет."""
    try:
        _write_state(task_id, "queued")
        _append_journal(task_id, "queued", f"резерв снят: {why}")
    except SystemExit as exc:  # строку успели поменять — назвать, а не молчать
        print(f"{task_id}: резерв снять не удалось ({exc})", file=sys.stderr)


def cmd_push(argv: list[str]) -> int:
    """Взять следующую: dry-run, затем кнопка ЛПР. Порядок не сокращается."""
    # Пауза проверяется ДО чтения страницы и до всякой правки строк: остановленная
    # очередь не должна оставлять следов, будто выдача начиналась. Код 6 выбран
    # не молчащим (в отличие от 3 и 4): слушатель на него шлёт уведомление, и
    # забытая пауза объявляет о себе сама, а не останавливает очередь незаметно —
    # тихий застой очереди контур уже проходил.
    if os.path.isfile(PAUSE_FILE):
        try:
            why = open(PAUSE_FILE, encoding="utf-8").read().strip()
        except OSError:
            why = ""
        print(f"очередь на паузе: {why or 'причина не указана'}\n"
              f"снять — удалить {PAUSE_FILE}", file=sys.stderr)
        return 6
    rows, broken = read_queue()
    if broken:
        print(f"внимание: {len(broken)} строк не разобрано, они пропущены — см. list")
    row = _next_queued(rows) if not argv else next(
        (r for r in rows if r["task_id"] == argv[0]), None)
    if not row:
        # Строки, стоящие не в очереди, называются вслух: молча «пусто» при
        # непустой таблице — это как раз то, как очередь вставала незамеченной.
        stuck = [f"{r['task_id']} ({r['action']})" for r in rows
                 if r["action"] in ("approved", "offered")]
        if stuck:
            print("в очереди нет задач, ждущих запуска; не в очереди стоят: "
                  f"{', '.join(stuck)}. `approved` починится ближайшим ВЗЯТИЕМ замка "
                  "(reconcile), `offered` ждёт кнопки — если кнопки нет, вернуть "
                  f"вручную: record {stuck[0].split()[0]} queued")
            return 3
        print("в очереди нет задач, ждущих запуска")
        return 3
    if row["action"] != "queued":
        print(f"{row['task_id']}: состояние «{row['action']}», не ждёт запуска"
              + (" — заявка уже создана и ждёт кнопки" if row["action"] == "offered" else ""))
        return 3

    # ЗАМОК БЕРЁТСЯ, А НЕ ОПРАШИВАЕТСЯ, И БЕРЁТСЯ ВСЕГДА — до всякой правки строки и
    # независимо от того, есть ли в таблице `approved`. Даёт это сразу две вещи.
    #
    # 1. Занятость. Держатель, не отражённый в таблице (ручная сессия, прогон вне
    #    очереди), раньше не мешал выдать вторую задачу поверх работающего роя.
    # 2. Взаимное исключение самой ВЫДАЧИ. Два `push` при свободном рое делали две
    #    кнопки на одну задачу, и приняв обе, её можно было запустить дважды. Теперь
    #    перевести строку в `offered` вправе только тот, кто в этот момент владеет
    #    замком, а владелец ровно один.
    state, why = hive_claim(row["task_id"])
    if state == "busy":
        print(f"рой занят — новая задача не выдаётся: {why}", file=sys.stderr)
        return 4
    if state != "claimed":
        print(f"состояние замка неизвестно, руками разобраться: {why}\n"
              f"замок: {HIVE_LOCK}\n"
              f"это НЕ «свободно» — пока не разобрано, задача не выдаётся", file=sys.stderr)
        return 5
    try:
        # Строка перечитывается ПОД ЗАМКОМ: между выбором и взятием её мог занять
        # кто-то другой, и решать по прочитанному раньше — та же ошибка, что судить
        # о занятости по вчерашнему status.
        fresh, _ = read_queue()
        now = next((r for r in fresh if r["task_id"] == row["task_id"]), None)
        if not now or now["action"] != "queued":
            print(f"{row['task_id']}: состояние сменилось на "
                  f"«{now['action'] if now else 'строки нет'}», пока брали замок")
            return 3
        row = now
        _reserve(row["task_id"])
    finally:
        # Замок отпускается ДО dry-run: dry-run — это тот же run_task.ps1, и он
        # берёт замок сам. Резерв строки к этому моменту уже сделан и держится не
        # замком, а состоянием `offered`, потому что покрыть замком нужно ровно
        # выдачу, а не ожидание кнопки: пока кнопка висит, рой ничем не занят, и
        # держать замок через раздумье человека значило бы врать о занятости.
        hive_release()

    try:
        return _offer(row)
    except BaseException as exc:   # SystemExit из _resolve — тоже отказ, а не выход
        _unreserve(row["task_id"], f"{type(exc).__name__}: {str(exc)[:150]}")
        raise


def _offer(row: dict) -> int:
    """Dry-run по зарезервированной строке и кнопка ЛПР. Отказ снимает резерв."""
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
        # Кнопки не будет — резерв обязан уйти вместе с ней, иначе строка застревает
        # в `offered` навсегда: ровно тот класс, из-за которого писалось задание.
        _unreserve(row["task_id"], f"dry-run не прошёл, код {dry.returncode}")
        return dry.returncode

    sent = subprocess.run(
        [sys.executable, APPROVE, "create", report, objective, work_dir, workers, priority,
         f"{row['task_id']} (очередь роя)"],
        capture_output=True, text=True, encoding="utf-8", errors="replace")
    print(sent.stdout.strip() or sent.stderr.strip())
    if sent.returncode != 0:
        _unreserve(row["task_id"], f"заявка не создана, код {sent.returncode}")
        return sent.returncode
    return 0


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

    # СВОЯ СТРОКА ВОССТАНАВЛИВАЕТСЯ ПО ЗАМКУ. Это и есть «одно хранилище авторитетно,
    # второе из него чинится»: замок называет, ЧЬЯ работа идёт, и если строка этого не
    # отражает (падение между взятием замка и записью строки, ручной прогон
    # `run_task -TaskId X` мимо очереди), она приводится в соответствие с замком, а не
    # наоборот. Записи вида «отработало» не трогаем: прогон мог быть закрыт заранее.
    ours = next((r for r in rows if r["task_id"] == mine), None)
    if ours and ours["action"] in ("queued", "offered"):
        _write_state(mine, "approved")
        _append_journal(mine, "approved",
                        f"строка восстановлена по замку pid {lock.get('pid')}: "
                        f"прогон идёт, а в таблице стояло «{ours['action']}»")
        print(f"{mine}: строка восстановлена по замку («{ours['action']}» → approved)")

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

    reserved = [{"task_id": "TASK-OBS-5", "action": "offered", "priority": "critical"}]
    assert _next_queued(reserved) is None, \
        "по зарезервированной строке заявка уже создана — второй кнопки быть не должно"
    assert _next_queued(reserved + sample)["task_id"] == "TASK-OBS-3", \
        "резерв не должен запирать остальную очередь"

    # Занятость. Проверка та же по строгости, что была у отбора: занят — не выдаём.
    # Плюс то, чего прежняя схема не умела вовсе: непрочитанный замок ≠ свободный.
    # Спрашивается теперь ответ `claim`, а не `status`: право даёт владение.
    assert _claim_state({"outcome": "busy"})[0] == "busy", "живой держатель — занято"
    assert _claim_state({"outcome": "claimed"})[0] == "claimed"
    assert _claim_state({"outcome": "claimed", "replacedStaleLock": True})[0] == "claimed", \
        "мёртвый держатель — мусор, замок перехватывается, а не считается занятостью"
    assert _claim_state({"outcome": "unknown"})[0] == "unknown"
    assert _claim_state({"outcome": "refused"})[0] == "unknown", "отказ — не «свободно»"
    assert _claim_state({"outcome": ""})[0] == "unknown", "без ответа — не «свободно»"
    assert _claim_state(None)[0] == "unknown", "вызов не отработал — не «свободно»"

    # Право чинить строку есть только у держателя, и только на ЧУЖИЕ строки.
    assert _stale_marks(busy, "TASK-OBS-4") == [], "свою строку держатель не снимает"
    assert _stale_marks(busy, "TASK-OBS-3") == ["TASK-OBS-4"], "чужая пережившая — снимается"
    assert _stale_marks(busy, "") == [], "замок без taskId — не трогаем ничего"
    # Пауза: выдача отказывает ДО чтения страницы и до замка. Проверяем на своём
    # временном файле — на боевом PAUSE_FILE проверка означала бы остановку живой
    # очереди ради теста.
    global PAUSE_FILE
    was = PAUSE_FILE
    try:
        with tempfile.NamedTemporaryFile("w", encoding="utf-8", suffix=".txt",
                                         delete=False) as fh:
            fh.write("проверка")
            PAUSE_FILE = fh.name
        assert cmd_push([]) == 6, "при паузе выдача обязана отказать кодом 6"
        os.unlink(PAUSE_FILE)
    finally:
        PAUSE_FILE = was

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
