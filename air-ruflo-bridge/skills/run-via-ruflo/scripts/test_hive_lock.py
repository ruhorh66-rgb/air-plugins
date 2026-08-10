r"""Проверки замка и выдачи очереди — те, которые unit-тестом не делаются.

Запуск: python test_hive_lock.py
Проверить на ПРЕЖНЕМ коде: HIVE_SINGLE=<путь к старой копии> python test_hive_lock.py

ЗАЧЕМ ОТДЕЛЬНЫЙ ФАЙЛ. `test_approve_queue.py` проверяет разбор и фильтры — то, что
считается в одном процессе. Здесь проверяется другое: что ДВА процесса не могут
получить один и тот же рой и что одна строка очереди не может дать две кнопки.
Unit-тест, не воспроизводящий гонку, даёт формальное покрытие при живом дефекте,
поэтому претенденты здесь настоящие: настоящий powershell, настоящий файл замка,
одновременный старт по общему сигналу.

ЖИВАЯ ОЧЕРЕДЬ НЕ УЧАСТВУЕТ. Страница и корень замка подменяются на временные
(`RUFLO_QUEUE_PAGE`, `RUFLO_REPORTS` и атрибуты модуля) — проверять правку очереди
на самой очереди значило бы портить то состояние, ради которого проверка и пишется.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import time
import types

sys.stdout.reconfigure(encoding="utf-8")

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
HIVE_SINGLE = os.environ.get("HIVE_SINGLE") or os.path.join(HERE, "hive_single.ps1")
RUN_TASK = os.path.join(HERE, "run_task.ps1")

PAGE_HEAD = """# полигон

## Очередь

| задача | состояние | задание | где работать | воркеров | приоритет | кто поставил |
|---|---|---|---|---|---|---|
"""
JOURNAL_HEAD = """
## Журнал исходов

| когда | задача | исход | чем подтверждено |
|---|---|---|---|
| 2026-01-01T00:00:00+06:00 | TASK-T-0 | done | строка журнала, существовавшая ДО прогона |
"""


def _page(tmp: str, rows: list[tuple[str, str, str]]) -> str:
    """Страница-полигон: (задача, состояние, приоритет) → файл, путь к нему."""
    objective = os.path.join(tmp, "obj.md")
    with open(objective, "w", encoding="utf-8") as fh:
        fh.write("полигонное задание")
    work = os.path.join(tmp, "work")
    os.makedirs(work, exist_ok=True)
    body = "".join(f"| {tid} | {state} | {objective} | {work} | 2 | {prio} | тест |\n"
                   for tid, state, prio in rows)
    path = os.path.join(tmp, "queue.md")
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(PAGE_HEAD + body + JOURNAL_HEAD)
    return path


def _state(page: str, task_id: str) -> str:
    with open(page, encoding="utf-8") as fh:
        for line in fh:
            if line.strip().startswith(f"| {task_id} "):
                return [c.strip() for c in line.strip().strip("|").split("|")][1]
    return ""


def _dead_pid() -> int:
    """PID процесса, которого уже нет, — чтобы замок был заведомо протухшим."""
    proc = subprocess.Popen([sys.executable, "-c", "pass"])
    proc.wait()
    return proc.pid


def _plant_stale(root: str) -> None:
    with open(os.path.join(root, "hive.lock.json"), "w", encoding="utf-8") as fh:
        json.dump({"owner": "прошлая сессия", "pid": _dead_pid(),
                   "pidStart": "2026-01-01T00:00:00.0000000Z",
                   "taskId": "TASK-T-OLD", "since": "2026-01-01T00:00:00.0000000+06:00",
                   "host": os.environ.get("COMPUTERNAME", "")}, fh)


def _claimants(root: str, count: int, hold_seconds: int = 3) -> list[str]:
    """Запустить `count` претендентов ОДНОВРЕМЕННО и вернуть их вывод.

    Старт по общему файлу-сигналу: без него powershell'ы разъезжаются на сотни
    миллисекунд запуска и «одновременности», которую проверяем, попросту нет.
    Победитель ЖИВЁТ ещё несколько секунд — иначе его замок протухает сразу и
    следующий претендент перехватывает его законно, а проверка ловит ложную гонку.
    """
    start = os.path.join(root, "GO")
    script = (f"$s='{start}'; while(-not (Test-Path -LiteralPath $s)) "
              f"{{ Start-Sleep -Milliseconds 5 }}; ")
    procs = []
    for i in range(count):
        cmd = (script + f"& '{HIVE_SINGLE}' -Action claim -Root '{root}' "
               f"-Owner 'c{i}' -TaskId 'TASK-T-{i}'; Start-Sleep -Seconds {hold_seconds}")
        procs.append(subprocess.Popen(["powershell.exe", "-NoProfile", "-ExecutionPolicy",
                                       "Bypass", "-Command", cmd],
                                      stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                      text=True, encoding="utf-8", errors="replace"))
    time.sleep(2.0)                      # дать всем дойти до ожидания сигнала
    open(start, "w").close()
    return [p.communicate()[0] or "" for p in procs]


def test_only_one_claimant_wins_over_stale_lock():
    """Два и больше одновременных претендента — замок достаётся РОВНО одному.

    Прежний код снимал протухший замок `Remove-Item`'ом и создавал свой. Все
    претенденты читали ОДИН протухший замок, и Remove-Item второго сносил уже НОВЫЙ
    файл первого: два держателя одного замка. Удаления больше нет — протухший
    перехватывается на месте под эксклюзивной ручкой.
    """
    for round_no in range(3):
        with tempfile.TemporaryDirectory() as tmp:
            _plant_stale(tmp)
            outs = _claimants(tmp, 6)
            claimed = [o for o in outs if '"outcome":"claimed"' in o]
            assert len(claimed) == 1, (
                f"круг {round_no}: замок достался {len(claimed)} претендентам вместо одного\n"
                + "\n".join(o.strip()[:300] for o in outs))
            busy = [o for o in outs if '"outcome":"busy"' in o]
            assert len(busy) == 5, f"круг {round_no}: проигравшие получили не отказ: {outs}"


def _module(tmp: str, page: str, buttons: list):
    """Модуль очереди, наведённый на полигон, с подменёнными dry-run и кнопкой."""
    import ruflo_queue as m
    m.PAGE = page
    m.REPORTS = tmp
    m.HIVE_LOCK = os.path.join(tmp, "hive.lock.json")
    m.HIVE_SINGLE = HIVE_SINGLE

    class _Sub:
        """Подменяется ТОЛЬКО то, что в тесте пускать нельзя: dry-run и Telegram.

        Замок вызывается по-настоящему: подменив его, мы проверяли бы заглушку.
        """
        DEVNULL = subprocess.DEVNULL

        def call(self, argv, **kw):
            return 10   # dry-run: предложение готово, человек не подтверждал

        def run(self, argv, **kw):
            if any("approve_via_telegram" in str(a) for a in argv):
                buttons.append(str(argv[-1]))     # заголовок заявки = «TASK-... (очередь роя)»
                return types.SimpleNamespace(returncode=0, stdout="кнопка отправлена", stderr="")
            return subprocess.run(argv, **kw)

    m.subprocess = _Sub()
    return m


def test_second_push_does_not_make_second_button():
    """Два подряд `push` не выдают две кнопки на одну задачу.

    Прежний код помечал строку только после нажатия, поэтому второй `push` при
    свободном рое видел ту же строку `queued` и делал ВТОРУЮ кнопку: приняв обе,
    одну задачу можно было запустить дважды (ERR-2026-000216 через другую дверь).
    """
    with tempfile.TemporaryDirectory() as tmp:
        page = _page(tmp, [("TASK-T-1", "queued", "critical"),
                           ("TASK-T-2", "queued", "normal")])
        buttons: list[str] = []
        m = _module(tmp, page, buttons)
        assert m.cmd_push([]) == 0
        assert m.cmd_push([]) == 0
        assert len(buttons) == len(set(buttons)) == 2, f"кнопки: {buttons}"
        assert _state(page, "TASK-T-1") == "offered"
        assert _state(page, "TASK-T-2") == "offered"
        # Третий раз выдавать нечего — и это не «пусто», а названный резерв.
        assert m.cmd_push([]) == 3
        assert len(buttons) == 2, f"третий push сделал лишнюю кнопку: {buttons}"


def test_push_refuses_while_manual_holder_alive():
    """Ручной держатель замка, все строки `queued` — заявка НЕ выдаётся."""
    with tempfile.TemporaryDirectory() as tmp:
        page = _page(tmp, [("TASK-T-1", "queued", "high")])
        buttons: list[str] = []
        m = _module(tmp, page, buttons)
        holder = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(40)"])
        try:
            got = subprocess.run(["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass",
                                  "-File", HIVE_SINGLE, "-Action", "claim", "-Root", tmp,
                                  "-Owner", "ручной прогон", "-HolderPid", str(holder.pid)],
                                 capture_output=True, text=True, encoding="utf-8")
            assert '"outcome":"claimed"' in got.stdout, got.stdout
            assert m.cmd_push([]) == 4, "очередь выдала заявку поверх работающего роя"
            assert not buttons, f"кнопка создана при занятом рое: {buttons}"
            assert _state(page, "TASK-T-1") == "queued", "строка тронута при отказе"
        finally:
            holder.kill()
            holder.wait()


def test_dead_holder_mark_is_cleared_and_task_handed_out():
    """`approved` без живого держателя: отметка снимается, задача снова выдаётся."""
    with tempfile.TemporaryDirectory() as tmp:
        page = _page(tmp, [("TASK-T-1", "approved", "high"), ("TASK-T-2", "queued", "high")])
        buttons: list[str] = []
        m = _module(tmp, page, buttons)
        # Замок ВЗЯТ нами под TASK-T-2 — значит по TASK-T-1 прогон уже не идёт.
        with open(m.HIVE_LOCK, "w", encoding="utf-8") as fh:
            json.dump({"owner": "тест", "pid": os.getpid(), "taskId": "TASK-T-2"}, fh)
        assert m.cmd_reconcile([str(os.getpid())]) == 0
        assert _state(page, "TASK-T-1") == "queued", "пережившая отметка не снята"
        os.remove(m.HIVE_LOCK)
        assert m.cmd_push(["TASK-T-1"]) == 0, "освобождённая задача снова не выдаётся"
        assert buttons == ["TASK-T-1 (очередь роя)"], buttons


def test_row_is_restored_from_the_lock():
    """Падение между взятием замка и записью строки — строка чинится ПО ЗАМКУ.

    Замок авторитетен: он называет, чья работа идёт. Строка — производная запись,
    и если она этого не отражает, приводится в соответствие с замком, а не наоборот.
    """
    with tempfile.TemporaryDirectory() as tmp:
        page = _page(tmp, [("TASK-T-1", "queued", "high")])
        m = _module(tmp, page, [])
        with open(m.HIVE_LOCK, "w", encoding="utf-8") as fh:
            json.dump({"owner": "run_task", "pid": os.getpid(), "taskId": "TASK-T-1"}, fh)
        assert m.cmd_reconcile([str(os.getpid())]) == 0
        assert _state(page, "TASK-T-1") == "approved", "строка не восстановлена по замку"
        # Чужому реконсилить нечего: право чинить — у держателя, а не у смотрящего.
        assert m.cmd_reconcile([str(os.getpid() + 1)]) == 4


def test_failed_run_releases_the_lock():
    """Отказ самого запуска: замок отпущен, следующая заявка возможна.

    Берётся НАСТОЯЩИЙ run_task.ps1 с заведомо падающим движком: важно, что замок
    снимается в `finally`, а не на счастливом пути.
    """
    with tempfile.TemporaryDirectory() as tmp:
        page = _page(tmp, [("TASK-T-1", "queued", "high")])
        cli = os.path.join(tmp, "fake-cli.js")
        with open(cli, "w", encoding="utf-8") as fh:
            fh.write("process.exit(3);\n")
        hive_root = os.path.join(tmp, "hiveroot")
        os.makedirs(hive_root, exist_ok=True)
        env = dict(os.environ, RUFLO_REPORTS=tmp, RUFLO_QUEUE_PAGE=page)
        res = subprocess.run(
            ["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", RUN_TASK,
             "-Objective", "полигон", "-TargetPath", os.path.join(tmp, "work"),
             "-ProjectRoot", hive_root, "-AllowForeignHive", "-CliPath", cli,
             "-ReportPath", os.path.join(tmp, "report.md"), "-TaskId", "TASK-T-1"],
            capture_output=True, text=True, encoding="utf-8", errors="replace", env=env)
        assert res.returncode != 0, f"падающий движок дал успех: {res.stdout}"
        assert not os.path.exists(os.path.join(tmp, "hive.lock.json")), \
            "замок остался после отказа запуска — очередь встанет навсегда"
        # Строка при этом восстановлена по замку (прогон начинался) — её закрывает
        # слушатель по коду выхода; здесь важно, что замок не заперт.
        assert _state(page, "TASK-T-1") in ("queued", "approved")


if __name__ == "__main__":
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    failed = 0
    for t in tests:
        started = time.time()
        try:
            t()
            print(f"ok  {t.__name__}  ({time.time() - started:.1f} c)")
        except AssertionError as exc:
            failed += 1
            print(f"FAIL {t.__name__}: {exc}")
    print(f"\n{len(tests) - failed} из {len(tests)} проверок пройдено")
    sys.exit(1 if failed else 0)
