"""Слушатель подтверждений: ловит нажатие кнопки в Telegram и запускает заявку.

Работает фоном (задача планировщика либо ручной запуск на время сессии).
Опрашивает Telegram long polling'ом, ждёт нажатия кнопки под заявкой, которую
положил `approve_via_telegram.py create`.

ЧТО ОН МОЖЕТ И ЧЕГО НЕ МОЖЕТ — это главное в файле.

МОЖЕТ: взять из очереди заявку с известным id и выполнить СОХРАНЁННУЮ В НЕЙ
команду — ту самую, что подготовил dry-run на этой машине.

НЕ МОЖЕТ, и это не настраивается:
  - выполнить текст, пришедший из чата. Сообщения не разбираются как команды
    вообще; читается только callback_data вида run:<id> / no:<id>;
  - выполнить что-либо по нажатию от чужого chat_id — сверка строгая, чужие
    нажатия игнорируются молча (ответ подтвердил бы существование канала);
  - выполнить заявку дважды или просроченную — статус переводится сразу,
    повторное нажатие получает отказ.

Если этот файл кто-то расширит до «выполнить произвольную команду из
сообщения» — механизм превратится из удобного гейта в удалённое исполнение
кода на SRVLM01 по факту владения токеном бота. Ограничение выше — не
перестраховка, а условие, при котором канал вообще допустим.
"""
from __future__ import annotations

import base64
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request

try:
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
except Exception:
    pass

SERVICE = "air-comms-telegram-bot"
QUEUE = r"E:\-4-\skill-state\ruflo-approvals"
TTL_SECONDS = 12 * 3600
OFFSET_FILE = os.path.join(QUEUE, "_offset.txt")


def _secret(account: str) -> str:
    import keyring
    value = keyring.get_password(SERVICE, account)
    if not value:
        raise SystemExit(f"нет {SERVICE}/{account} в keyring")
    return value


def _api(method: str, payload: dict | None = None, timeout: int = 70) -> dict:
    token = _secret("botfather-token")
    url = f"https://api.telegram.org/bot{token}/{method}"
    data = json.dumps(payload).encode("utf-8") if payload else None
    headers = {"Content-Type": "application/json"} if data else {}
    req = urllib.request.Request(url, data=data, headers=headers,
                                 method="POST" if data else "GET")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        return {"ok": False, "error": exc.read().decode("utf-8", "replace")[:200]}
    except Exception as exc:  # сеть моргнула — не роняем слушателя
        return {"ok": False, "error": f"{type(exc).__name__}: {exc}"}


def _answer(callback_id: str, text: str) -> None:
    """Всплывающее уведомление на кнопку. show_alert=True — заметно.

    Без него Telegram показывает еле заметную полоску вверху, и ЛПР
    справедливо сказал, что «не совсем явно видно, прошла ли заявка».
    """
    _api("answerCallbackQuery",
         {"callback_query_id": callback_id, "text": text, "show_alert": True}, timeout=20)


def _edit(chat_id, message_id: int, text: str, keep_buttons: bool = False) -> None:
    """Переписать само сообщение с заявкой — чтобы его вид отражал состояние.

    Кнопка, которая продолжает висеть после нажатия, выглядит как «ничего не
    произошло»: именно это и наблюдал ЛПР. После решения кнопки снимаются, а
    текст сообщения становится статусом — заявка перестаёт выглядеть свежей.
    """
    payload = {"chat_id": int(chat_id), "message_id": message_id, "text": text}
    if not keep_buttons:
        payload["reply_markup"] = {"inline_keyboard": []}
    _api("editMessageText", payload, timeout=20)


def _notify(text: str) -> None:
    try:
        _api("sendMessage", {"chat_id": int(_secret("chat_id")), "text": text}, timeout=20)
    except Exception:
        pass


def _load_offset() -> int:
    try:
        with open(OFFSET_FILE, encoding="utf-8") as fh:
            return int(fh.read().strip() or 0)
    except (OSError, ValueError):
        return 0


def _save_offset(value: int) -> None:
    os.makedirs(QUEUE, exist_ok=True)
    try:
        with open(OFFSET_FILE, "w", encoding="utf-8") as fh:
            fh.write(str(value))
    except OSError:
        pass


RUN_TASK = os.path.join(os.path.dirname(os.path.abspath(__file__)), "run_task.ps1")

# ДВИЖОК ЗДЕСЬ НЕ РЕШАЕТСЯ ВОВСЕ — его выбирает run_task.ps1 в момент запуска.
#
# 13.08.2026 наступили на те же грабли ТРЕТИЙ раз, и по-новому: константы уже не было,
# путь брался из ruflo_engine — но НА ИМПОРТЕ. Слушатель живёт сутками; этот прочитал
# engine.json, когда там стояла 3.36.0, и продолжал передавать её в -CliPath после того,
# как контур перешёл на 3.38.8. Прогон TASK-OBS-0053 в итоге координировался 3.38.8
# (MCP-сервер поднимается по .mcp.json), а исполнялся 3.36.0.
#
# Урок класса: единый источник не помогает, если потребитель читает его ОДИН РАЗ.
# Долгоживущий процесс обязан спрашивать заново на каждом решении — либо не спрашивать
# совсем. Здесь выбрано второе: -CliPath больше не передаётся, run_task разрешает
# версию сам, в секунду запуска.
QUEUE_SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "ruflo_queue.py")


def _task_id_of(title: str) -> str:
    """Идентификатор строки очереди из заголовка заявки; пусто — заявка не из очереди.

    Заголовок сверяется ЦЕЛИКОМ, а не по началу. `\\b` после идентификатора пропускал
    `TASK-OBS-0043-черновик` как `TASK-OBS-0043` — отметка легла бы на чужую строку
    очереди (codex review 09.08.2026). Формат задаёт `cmd_push`, и совпадать он обязан
    полностью: всё остальное очередью не выдавалось.
    """
    match = re.fullmatch(r"(TASK-[A-Z]+-\d+) \(очередь роя\)", (title or "").strip())
    return match.group(1) if match else ""


def _queue_record(title: str, action: str, proof: str) -> str:
    """Отметить состояние строки очереди. Возвращает описание сбоя либо пустую строку.

    ПОЧЕМУ ЭТО ЗДЕСЬ, А НЕ В `ruflo_queue.py push`. Толкающая сторона знает только,
    что кнопка ОТПРАВЛЕНА; о нажатии и об исходе прогона узнаёт слушатель. Пока
    состояние писала не она, строка оставалась `queued` всё время работы роя — и
    защита «пока одна в работе, вторая не выдаётся» не срабатывала вовсе, потому
    что она смотрит на `approved`, которого никто не ставил. Так TASK-OBS-0043 была
    подтверждена трижды: 20:41, 20:55, 21:05.

    Заявки бывают и не из очереди (ручные прогоны air-watch) — у них в заголовке нет
    идентификатора, и тогда отмечать нечего. Отсутствие строки не ошибка.
    """
    task_id = _task_id_of(title)
    if not task_id:
        return ""
    try:
        res = subprocess.run([sys.executable, QUEUE_SCRIPT, "record", task_id,
                              action, proof],
                             capture_output=True, text=True, encoding="utf-8",
                             errors="replace", timeout=60)
    except Exception as exc:  # очередь недоступна — прогон из-за этого не отменяем
        return f"состояние {task_id} не записано: {exc}"
    if res.returncode != 0:
        return (f"состояние {task_id} не записано: "
                f"{(res.stderr or res.stdout or '').strip()[:200]}")
    return ""


def _safe_path(value: str) -> bool:
    """Годится ли строка как путь в аргументе PowerShell.

    Абсолютный путь на локальном диске, без кавычек, точки с запятой, переводов
    строки и подстановочных символов. UNC-пути (`\\\\сервер\\шара`) отвергаются
    намеренно: подтверждённый кнопкой запуск не должен уметь тянуть цель с
    чужой машины.
    """
    if not value or not os.path.isabs(value) or value.startswith("\\\\"):
        return False
    return not any(ch in value for ch in '"\'`;|&\r\n$*?<>')


def _status_of(path: str) -> str:
    """Состояние заявки из файла. Нечитаемый файл не считаем ждущим решения."""
    try:
        with open(path, encoding="utf-8") as fh:
            data = json.load(fh)
        return str(data.get("status", "")) if isinstance(data, dict) else ""
    except (OSError, ValueError):
        return ""


def _active_requests() -> list[str]:
    """Одобренные заявки, чей outcome ещё должен собрать listener."""
    active = []
    try:
        names = os.listdir(QUEUE)
    except OSError:
        return active
    for name in names:
        if not name.endswith(".json"):
            continue
        try:
            with open(os.path.join(QUEUE, name), encoding="utf-8") as fh:
                req = json.load(fh)
        except (OSError, ValueError):
            continue
        if (isinstance(req, dict) and req.get("status") == "approved"
                and req.get("run_state") in (None, "", "waiting", "running")):
            active.append(name)
    return active


def _listener_run_in_progress(req: dict) -> bool:
    """Не дать второму detached launcher обогнать ещё не записанный hive.lock."""
    try:
        names = os.listdir(QUEUE)
    except OSError:
        return False
    for name in names:
        if not name.endswith(".json") or name == f"{req['id']}.json":
            continue
        try:
            with open(os.path.join(QUEUE, name), encoding="utf-8") as fh:
                other = json.load(fh)
        except (OSError, ValueError):
            continue
        if (isinstance(other, dict) and other.get("status") == "approved"
                and other.get("run_state") == "running"):
            return True
    return False


# Сколько ждать освобождения роя перед отложенным запуском. Три часа: столько живёт
# самый долгий из наших прогонов с запасом. Больше — уже не «подхватит следующую», а
# «запустит неизвестно когда».
DEFERRED_START_LIMIT_S = 3 * 60 * 60
HIVE_LOCK_FILE = os.path.join(r"E:\-4-\ruflo-hive", "hive.lock.json")


def _request_path(req: dict) -> str:
    return os.path.join(QUEUE, f"{req['id']}.json")


def _save_request(req: dict) -> None:
    """Атомарно сохранить служебное состояние запуска рядом с заявкой."""
    path = _request_path(req)
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as fh:
        json.dump(req, fh, ensure_ascii=False, indent=1)
    os.replace(tmp, path)


def _run_files(req: dict) -> tuple[str, str, str]:
    """Файлы одного отцепленного запуска; id очищен только для имени файлов."""
    safe_id = re.sub(r"[^A-Za-z0-9_-]", "_", str(req["id"]))
    root = os.path.join(QUEUE, f"_run_{safe_id}")
    return root + ".stdout.log", root + ".stderr.log", root + ".exit"


def _hive_busy() -> bool:
    """Занят ли рой ПРЯМО СЕЙЧАС — по единственному источнику, замку в корне.

    Мёртвый держатель занятостью не считается: `run_task.ps1` перехватывает такой замок
    сам при claim. Здесь достаточно факта наличия файла — если он протух, следующий
    claim это и разберёт, а мы не начнём второй прогон поверх живого.
    """
    return os.path.isfile(HIVE_LOCK_FILE)


def _run_task_argv(req: dict) -> list[str]:
    """Собрать argv `run_task.ps1` ИЗ ПАРАМЕТРОВ уже проверенной заявки."""
    argv = [
        "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", RUN_TASK,
        "-ObjectiveFile", req["objective_file"],
        "-TargetPath", req["target_path"],
        "-ReportPath", req["report"],
        "-Workers", str(int(req.get("workers", 5))),
        "-Priority", str(req.get("priority", "high")),
        "-Approval", "I_APPROVE_RUFLO_PLAN",
        "-ApprovalFile", _request_path(req),
    ]
    task_id = _task_id_of(req.get("title", ""))
    if task_id:
        argv += ["-TaskId", task_id]
    return argv


def _quote_ps(value: str) -> str:
    """Строка PowerShell; проверенные пути сами по себе не содержат апострофов."""
    return "'" + value.replace("'", "''") + "'"


def _launch_detached(req: dict) -> None:
    """Запустить PowerShell отдельно от listener и оставить ему durable exit-marker."""
    stdout_path, stderr_path, exit_path = _run_files(req)
    try:
        os.remove(exit_path)
    except FileNotFoundError:
        pass
    argv = _run_task_argv(req)
    # run_task использует `exit` на многих ветках, поэтому запускается дочерним
    # PowerShell. Wrapper остаётся независимым процессом и после дочернего выхода
    # атомарно пишет код: listener может прочесть marker после собственного рестарта,
    # когда объекта Popen уже нет.
    ps_args = " ".join(_quote_ps(str(value)) for value in argv[6:])
    wrapper = (
        f"& powershell.exe -NoProfile -ExecutionPolicy Bypass -File {_quote_ps(RUN_TASK)} {ps_args}\n"
        "$code = $LASTEXITCODE\n"
        f"[System.IO.File]::WriteAllText({_quote_ps(exit_path)}, [string]$code, "
        "(New-Object System.Text.UTF8Encoding($false)))\n"
        "exit $code\n"
    )
    encoded = base64.b64encode(wrapper.encode("utf-16le")).decode("ascii")
    creationflags = 0
    if os.name == "nt":
        creationflags = (getattr(subprocess, "DETACHED_PROCESS", 0x00000008)
                         | getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0x00000200))
    try:
        with open(stdout_path, "wb") as stdout, open(stderr_path, "wb") as stderr:
            subprocess.Popen(
                ["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass",
                 "-EncodedCommand", encoded],
                stdout=stdout, stderr=stderr, creationflags=creationflags,
            )
    except Exception as exc:
        _queue_record(req.get("title", ""), "failed",
                      f"запуск не состоялся: {type(exc).__name__}: {str(exc)[:200]}")
        _notify(f"⚠ Заявка {req['id']}: запуск не состоялся — "
                f"{type(exc).__name__}: {str(exc)[:200]}")
        req["run_state"] = "launch_failed"
        _save_request(req)
        return
    req.update({
        "run_state": "running",
        "run_started_at": time.time(),
        "run_stdout": stdout_path,
        "run_stderr": stderr_path,
        "run_exit": exit_path,
    })
    _save_request(req)


def _read_exit_code(path: str) -> int | None:
    try:
        with open(path, encoding="utf-8") as fh:
            value = fh.read().strip()
    except OSError:
        return None
    return int(value) if re.fullmatch(r"-?\d+", value) else None


def _log_tail(path: str, limit: int = 64 * 1024) -> str:
    try:
        with open(path, "rb") as fh:
            fh.seek(0, os.SEEK_END)
            fh.seek(max(0, fh.tell() - limit))
            return fh.read().decode("utf-8", "replace").strip()
    except OSError:
        return ""


def _finish_detached_run(req: dict, returncode: int) -> None:
    """Отразить durable outcome завершившегося отцепленного процесса."""
    took = int(time.time() - float(req.get("run_started_at", time.time())))
    mins = f"{took // 60} мин {took % 60} с" if took >= 60 else f"{took} с"
    out = _log_tail(str(req.get("run_stdout", "")))
    codes = {
        0: "✅ Рой отработал",
        4: "ℹ Рой уже занят другой задачей — эта присоединится к очереди",
        6: "⚠ Не запущено: MCP не зарегистрирован для канонического роя",
        7: "⚠ Не запущено: движок не нашёл Claude Code (тихая деградация)",
        8: "⚠ Не запущено: истекла авторизация Claude Code — нужен claude auth login",
        9: "⚠ Queen-сессия завершилась с ошибкой",
        11: "⚠ Отработало, НО роя не было: ни одного вызова mcp__claude-flow__",
    }
    head = codes.get(returncode, f"⚠ Код выхода {returncode}")
    verdict = ""
    for line in out.splitlines():
        if "Рой работал" in line or "РОЯ НЕ БЫЛО" in line or "SECRET SCAN" in line:
            verdict += "\n" + line.strip()
    if returncode == 4:
        state_note = _queue_record(
            req.get("title", ""), "queued",
            "возвращена в очередь: рой был занят другой задачей, прогон не начинался"
        )
    else:
        closing = "done" if returncode == 0 else "failed"
        state_note = _queue_record(
            req.get("title", ""), closing,
            f"{head}; заняло {mins}; отчёт {req.get('report', '')}")
    req["run_state"] = "finished"
    req["run_returncode"] = returncode
    req["run_finished_at"] = time.time()
    _save_request(req)
    _notify(f"{head} — заявка {req['id']}, заняло {mins}{verdict[:600]}\n\n"
            f"Отчёт: {req.get('report', '')}"
            + (f"\n\n⚠ Очередь: {state_note}" if state_note else ""))
    if returncode != 4:
        _push_next()


def _advance_run(req: dict) -> None:
    """Один неблокирующий шаг отложенного или уже запущенного прогона."""
    state = str(req.get("run_state") or "waiting")
    if state == "running":
        code = _read_exit_code(str(req.get("run_exit", "")))
        if code is not None:
            _finish_detached_run(req, code)
        return
    if state != "waiting":
        return
    now = time.time()
    deadline = float(req.get("deferred_deadline", now + DEFERRED_START_LIMIT_S))
    if now >= deadline:
        req["run_state"] = "deferred_expired"
        _save_request(req)
        _notify(f"⚠ Заявка {req['id']}: рой не освободился за "
                f"{DEFERRED_START_LIMIT_S // 3600} ч — НЕ запускаю. Подтвердить заново.")
        return
    if _hive_busy() or _listener_run_in_progress(req):
        if not req.get("deferred_notified"):
            _notify(f"⏳ Заявка {req['id']}: рой занят, запуск отложен до освобождения "
                    f"(жду до {DEFERRED_START_LIMIT_S // 3600} ч)")
            req["deferred_notified"] = True
            _save_request(req)
        return
    state_note = _queue_record(req.get("title", ""), "approved",
                               f"заявка {req['id']} подтверждена кнопкой, прогон начат")
    if state_note:
        req["queue_start_note"] = state_note
    _launch_detached(req)


def _run_request(req: dict) -> None:
    """Проверить заявку и запланировать её неблокирующий запуск.

    ЗДЕСЬ НЕ СТРОИТСЯ СТРОКА КОМАНДЫ. Переписано 08.08.2026 после codex review,
    который нашёл blocker: прежняя версия склеивала `powershell -Command` из
    значений заявки, и поле `report` попадало внутрь кавычек без проверки —
    значение с `"` и `;` давало исполнение произвольного PowerShell по нажатию
    кнопки. Заявленная гарантия «строки команды в заявке нет» была неправдой:
    строка была, просто собиралась не полностью из заявки, а наполовину.

    Теперь `-File` и argv-список: PowerShell получает путь скрипта и значения
    отдельными аргументами, склеивать нечего. Цель передаётся ПУТЁМ
    (`-ObjectiveFile`), а не подставленным содержимым — раньше её приходилось
    разворачивать через `(Get-Content ...)`, что и требовало режима -Command.

    Пути дополнительно проверяются `_safe_path`: заявка могла быть создана
    давно, а её значения — прийти из более старой версии кода без проверок.
    """
    objective_file = req.get("objective_file", "")
    target = req.get("target_path", "")
    report = req.get("report", "")
    workers = int(req.get("workers", 5))
    priority = str(req.get("priority", "high"))
    if not os.path.isfile(objective_file) or not os.path.isdir(target):
        _notify(f"⚠ Заявка {req['id']}: цель или каталог задачи исчезли, не запускаю")
        return
    if not (1 <= workers <= 32) or priority not in ("normal", "high", "critical"):
        _notify(f"⚠ Заявка {req['id']}: недопустимые параметры, не запускаю")
        return
    bad = [n for n, v in (("objective_file", objective_file), ("target_path", target),
                          ("report", report)) if not _safe_path(v)]
    if bad or not os.path.isdir(os.path.dirname(report)):
        _notify(f"⚠ Заявка {req['id']}: путь не проходит проверку ({', '.join(bad) or 'report'}), "
                f"не запускаю")
        return
    # ПОДПИСЬ ПРОВЕРЯЕТСЯ ЗДЕСЬ, А НЕ ТОЛЬКО ПРИ СОЗДАНИИ ЗАЯВКИ.
    #
    # Найдено 14.08.2026 на живом прогоне TASK-OBS-0055 (ERR-2026-000237). Заявка несла
    # и `sig`, и `objective_sha256`, проверка для них была написана и лежала рядом —
    # `approve_via_telegram.verify_request`, — но слушатель её не звал: нажал человек,
    # значит запускаем. В тот день файл цели правили ПОСЛЕ подписи (синхронизация шапок),
    # и прогон ушёл по тексту, отличному от подтверждённого.
    #
    # Проверять при создании бессмысленно: между кнопкой и нажатием проходят часы, и
    # смысл подписи ровно в этом промежутке. Отсюда место проверки — момент исполнения.
    #
    # Проверка идёт ДО ожидания свободного роя: ждать три часа ради отказа незачем.
    try:
        from approve_via_telegram import verify_request
        ok, why = verify_request(req)
    except Exception as exc:  # noqa: BLE001 — недоступна проверка = не годна
        ok, why = False, f"проверить подпись не смог: {type(exc).__name__}: {exc}"
    if not ok:
        _notify(f"⚠ Заявка {req['id']}: НЕ запускаю — {why}. "
                f"Подтвердить заново по актуальному тексту.")
        return

    # Состояние ожидания хранится в заявке: после рестарта listener продолжит ждать
    # тот же срок, а не потеряет уже данное человеком разрешение. Ни sleep, ни wait
    # здесь нет — следующий poll-loop сам сделает один короткий шаг.
    if not req.get("run_state"):
        req["run_state"] = "waiting"
        req["deferred_started_at"] = time.time()
        req["deferred_deadline"] = req["deferred_started_at"] + DEFERRED_START_LIMIT_S
        _save_request(req)
    _advance_run(req)


def _service_runs() -> None:
    """На каждом poll-loop обслужить ожидания и completion marker всех заявок."""
    try:
        names = [name for name in os.listdir(QUEUE) if name.endswith(".json")]
    except OSError:
        return
    for name in names:
        path = os.path.join(QUEUE, name)
        try:
            with open(path, encoding="utf-8") as fh:
                req = json.load(fh)
        except (OSError, ValueError):
            continue
        if not isinstance(req, dict) or req.get("status") != "approved":
            continue
        # Уже запущенный процесс не надо заново валидировать: файл цели мог
        # законно измениться ПОСЛЕ его старта, а completion marker всё равно
        # обязан попасть в очередь и Telegram. До запуска (waiting/без state)
        # подпись проверяется через _run_request на каждом восстановлении.
        if req.get("run_state") == "running":
            _advance_run(req)
        elif req.get("run_state") in (None, "", "waiting"):
            _run_request(req)


def _push_next() -> None:
    """Выдать следующую задачу очереди сразу после закрытия предыдущей.

    Иначе очередь стоит: сессия, поставившая строку, могла закончиться часы назад,
    и ждать, пока кто-то вспомнит подать `push`, — значит не иметь очереди вовсе.
    Так и вышло 08.08.2026: рой отработал, следующая заявка не пришла.

    Гейт ЛПР это не отменяет: `push` доходит только до кнопки, запускает по-прежнему
    нажатие. Задача занята, испорчена или очередь пуста — скрипт сам вернёт код 3
    либо 4, и это нормальный исход, а не сбой.

    Уведомляем ТОЛЬКО о неожиданном: новая заявка объявляет о себе сама, отдельным
    сообщением с кнопкой, а «очередь пуста» после каждого прогона было бы шумом.
    """
    try:
        res = subprocess.run([sys.executable, QUEUE_SCRIPT, "push"],
                             capture_output=True, text=True, encoding="utf-8",
                             errors="replace", timeout=1200)
    except Exception as exc:
        _notify(f"⚠ Очередь: следующую задачу выдать не удалось — {exc}")
        return
    if res.returncode in (0, 3, 4):
        return
    _notify(f"⚠ Очередь: следующая задача не выдана, код {res.returncode}\n"
            f"{((res.stderr or res.stdout) or '').strip()[-400:]}")


# ПРИЗНАК ЖИЗНИ И ГРОМКИЙ ОТКАЗ (ERR-2026-000242, три случая за двое суток).
#
# Прежняя редакция глотала ошибку дважды подряд, и оба места задумывались как
# устойчивость: `_api` на любое исключение возвращал {"ok": false, "error": ...} с
# комментарием «сеть моргнула — не роняем слушателя», а `poll_once` на «не ок» спал
# пять секунд и молчал. Поле error не читал никто. Получилось «пережить ЛЮБОЙ отказ
# навсегда, не сказав ни слова»: процесс жив, журнал молчит, смещение стоит, а ЛПР
# третий раз жмёт кнопку и думает, что сломана она.
#
# Лечится не таймаутом — он там и был (urlopen timeout=70). Лечится тем, что отказ
# перестаёт быть тихим: причина в журнал сразу, а после порога — В TELEGRAM, чтобы
# человек узнавал о поломке от системы, а не по своему третьему нажатию.
_fail_streak = 0
_last_ok_at = time.time()
_cycle = 0
_first_poll_done = False
FAIL_LOUD_AT = 3          # после скольких подряд неудач сказать вслух
FAIL_REPEAT_EVERY = 60    # и повторять не чаще, чем раз в столько неудач
HEARTBEAT_EVERY = 40      # строка «жив, последний успешный опрос тогда-то»


def poll_once(chat_id: str) -> int:
    global _fail_streak, _last_ok_at, _cycle, _first_poll_done
    _cycle += 1
    # Heartbeat относится к самому живому циклу, а не к исходу getUpdates или
    # завершению роя: долгий detached run больше не может сделать журнал немым.
    if _cycle % HEARTBEAT_EVERY == 0:
        print(f"жив, цикл {_cycle}, последний успешный опрос "
              f"{time.strftime('%H:%M:%S', time.localtime(_last_ok_at))}", flush=True)
    _service_runs()
    offset = _load_offset()
    resp = _api("getUpdates", {"offset": offset, "timeout": 50, "allowed_updates": ["callback_query"]})
    if not resp.get("ok"):
        _fail_streak += 1
        why = str(resp.get("error") or "причина не названа")[:200]
        print(f"опрос не прошёл ({_fail_streak} подряд): {why}", flush=True)
        if _fail_streak == FAIL_LOUD_AT or (
                _fail_streak > FAIL_LOUD_AT and _fail_streak % FAIL_REPEAT_EVERY == 0):
            mins = int((time.time() - _last_ok_at) / 60)
            _notify(f"⚠ Слушатель не может опросить Telegram: {_fail_streak} неудач подряд, "
                    f"последний успешный опрос {mins} мин назад.\nПричина: {why}\n"
                    f"Кнопка сейчас НЕ работает — нажатие не потеряется, но и не сработает, "
                    f"пока слушатель не восстановится.")
        time.sleep(5)
        return 0
    if _fail_streak:
        print(f"опрос восстановлен после {_fail_streak} неудач подряд", flush=True)
        _fail_streak = 0
    _last_ok_at = time.time()
    # НАКОПЛЕННОЕ НАЖАТИЕ. Первый опрос после старта забирает всё, что скопилось, пока
    # слушателя не было; исполнять это молча нельзя — человек нажимал в другой
    # обстановке и уже решил, что кнопка не работает.
    queued = (not _first_poll_done) and bool(resp.get("result"))
    _first_poll_done = True
    if queued:
        print("первый опрос забрал накопленные нажатия — они сделаны ДО запуска "
              "этого слушателя", flush=True)
        _notify("ℹ️ Нажатие было сделано до запуска слушателя и пролежало в очереди "
                "Telegram. Выполняю его сейчас — если обстановка изменилась, "
                "остановите прогон.")
    handled = 0
    for upd in resp.get("result", []):
        _save_offset(upd["update_id"] + 1)
        cq = upd.get("callback_query")
        if not cq:
            continue
        # ЧУЖОЙ ОТПРАВИТЕЛЬ — молча мимо, без ответа.
        sender = str((cq.get("from") or {}).get("id", ""))
        if sender != str(chat_id):
            continue
        data = str(cq.get("data") or "")
        if ":" not in data:
            continue
        action, rid = data.split(":", 1)
        path = os.path.join(QUEUE, f"{rid}.json")
        if not os.path.isfile(path):
            _answer(cq["id"], "Заявка не найдена")
            continue
        with open(path, encoding="utf-8") as fh:
            req = json.load(fh)
        if req.get("status") != "pending":
            _answer(cq["id"], f"Уже {req.get('status')}")
            continue
        msg = cq.get("message") or {}
        mid = msg.get("message_id")
        title = req.get("title", "Запуск роя")
        stamp = time.strftime("%H:%M")
        if time.time() - req.get("created_at", 0) > TTL_SECONDS:
            req["status"] = "expired"
            _answer(cq["id"], "Заявка просрочена — подтвердить уже нельзя")
            if mid:
                _edit(sender, mid, f"⌛ {title}\n\nЗаявка {rid} просрочена, не запущена.")
        elif action == "run":
            req["status"] = "approved"
            _answer(cq["id"], "Принято. Запускаю рой — итог придёт сюда же.")
            if mid:
                # Исходное сообщение гасим до короткой строки: его задача теперь —
                # не выглядеть живой заявкой, на которую ещё можно нажать.
                _edit(sender, mid, f"🐝 {title}\n✅ подтверждено в {stamp}, заявка {rid}")
            # Отдельным сообщением, а не только правкой прежнего: правка приходит в тот
            # же пузырь, где была кнопка, и её принимают за исходную заявку — ЛПР так и
            # сказал, что кнопки «нет», когда она уже была нажата, а сообщение изменено.
            # Новое сообщение поднимает уведомление на телефоне и не путается с прежним.
            _notify(f"🐝 ПРИНЯТО В РАБОТУ — {title}\n"
                    f"────────────────\n"
                    f"Заявка {rid}, воркеров {req.get('workers')}, "
                    f"приоритет {req.get('priority', 'high')}.\n"
                    f"Рой запущен в {stamp}. Итог придёт сюда же — ждать не нужно.")
        elif action == "no":
            req["status"] = "rejected"
            _answer(cq["id"], "Отклонено. Рой не запускается.")
            if mid:
                _edit(sender, mid, f"✖ {title}\n✖ отклонено в {stamp}, заявка {rid}")
            # Отказ отдельным сообщением не дублируем: он ничего не запускает, и
            # уведомление о нём было бы шумом. Правки исходного сообщения достаточно.
        else:
            continue
        # Отклонённая и просроченная заявка ОСВОБОЖДАЕТ строку очереди. `push`
        # резервирует её (`offered`) до создания кнопки — иначе два push делают две
        # кнопки на одну задачу, — и снять этот резерв может только тот, кто знает
        # решение человека, то есть слушатель. Не снять его значит запереть задачу
        # молча, а молчаливый затык — ровно тот класс, ради которого всё это писалось.
        if req["status"] in ("rejected", "expired"):
            note = "отклонена" if req["status"] == "rejected" else "просрочена"
            state_note = _queue_record(
                title, "queued", f"заявка {rid} {note} — резерв снят, строка вернулась в очередь")
            if state_note:
                _notify(f"⚠ Очередь: {state_note}")
        req["decided_at"] = time.time()
        req["message_id"] = mid
        # Атомарно: сводка в approve_via_telegram.py читает этот же каталог и
        # при обычной перезаписи могла бы поймать файл на середине и молча
        # пропустить заявку (codex review 08.08.2026, minor).
        tmp = path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as fh:
            json.dump(req, fh, ensure_ascii=False, indent=1)
        os.replace(tmp, path)
        handled += 1
        if req["status"] == "approved":
            _run_request(req)
    return handled


def main() -> int:
    """Два режима.

    Без аргументов — постоянный слушатель (задача планировщика).
    `--once <минуты>` — РАЗОВЫЙ: ждёт решения по текущим заявкам заданное время
    и завершается. ЛПР предпочёл этот режим: постоянный процесс ради кнопки,
    нажимаемой несколько раз в день, — лишняя сущность в контуре. Разовый
    живёт ровно столько, сколько длится ожидание ответа на конкретную заявку.
    """
    chat_id = _secret("chat_id")
    os.makedirs(QUEUE, exist_ok=True)
    once = "--once" in sys.argv
    deadline = None
    if once:
        idx = sys.argv.index("--once")
        minutes = int(sys.argv[idx + 1]) if len(sys.argv) > idx + 1 else 30
        deadline = time.time() + minutes * 60
        print(f"разовый обработчик: жду решения {minutes} мин", flush=True)
    else:
        print(f"слушатель запущен, очередь: {QUEUE}", flush=True)
    while True:
        try:
            handled = poll_once(chat_id)
            if once:
                active = _active_requests()
                if handled:
                    # Закрытие задачи выдаёт следующую (`_push_next`), и её кнопку
                    # ловить некому, если выйти прямо сейчас. Выходим, когда ждать
                    # больше нечего: неотвеченных заявок и запущенных/отложенных
                    # прогонов не осталось. Раньше `_run_request` сам блокировал
                    # этот режим до исхода; теперь его outcome собирает poll-loop.
                    pending = [f for f in os.listdir(QUEUE) if f.endswith(".json")
                               and _status_of(os.path.join(QUEUE, f)) == "pending"]
                    active = _active_requests()
                    if not pending and not active:
                        print("решение получено и обработано", flush=True)
                        return 0
                    print(f"обработано; ждут решения: {len(pending)}, "
                          f"прогонов в работе/ожидании: {len(active)}", flush=True)
                    deadline = time.time() + minutes * 60
                if deadline and time.time() > deadline and not active:
                    print("время ожидания истекло, решения не было", flush=True)
                    return 3
        except KeyboardInterrupt:
            print("остановлен", flush=True)
            return 0
        except Exception as exc:  # noqa: BLE001 — обработчик не должен умирать
            print(f"сбой цикла: {type(exc).__name__}: {exc}", flush=True)
            time.sleep(10)


if __name__ == "__main__":
    sys.exit(main())
