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
CLI_PATH = (r"E:\-4-\ruflo-pilot\.npm-cache-3.34.0\_npx\2ed56890c96f58f7"
            r"\node_modules\@claude-flow\cli\bin\cli.js")
QUEUE_SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "ruflo_queue.py")


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
    # Заголовок сверяется ЦЕЛИКОМ, а не по началу. `\b` после идентификатора
    # пропускал `TASK-OBS-0043-черновик` как `TASK-OBS-0043` — отметка легла бы на
    # чужую строку очереди (codex review 09.08.2026). Формат задаёт `cmd_push`, и
    # совпадать он обязан полностью: всё остальное очередью не выдавалось.
    match = re.fullmatch(r"(TASK-[A-Z]+-\d+) \(очередь роя\)", (title or "").strip())
    if not match:
        return ""
    try:
        res = subprocess.run([sys.executable, QUEUE_SCRIPT, "record", match.group(1),
                              action, proof],
                             capture_output=True, text=True, encoding="utf-8",
                             errors="replace", timeout=60)
    except Exception as exc:  # очередь недоступна — прогон из-за этого не отменяем
        return f"состояние {match.group(1)} не записано: {exc}"
    if res.returncode != 0:
        return (f"состояние {match.group(1)} не записано: "
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


def _run_request(req: dict) -> None:
    """Собрать вызов run_task.ps1 ИЗ ПАРАМЕТРОВ заявки и выполнить.

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
    argv = [
        "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", RUN_TASK,
        "-ObjectiveFile", objective_file,
        "-TargetPath", target,
        "-CliPath", CLI_PATH,
        "-ReportPath", report,
        "-Workers", str(workers),
        "-Priority", priority,
        "-Approval", "I_APPROVE_RUFLO_PLAN",
    ]
    # Строка очереди помечается ДО запуска, а не после: пока рой работает, очередь
    # обязана показывать его занятым, иначе соседняя сессия выдаст вторую задачу.
    state_note = _queue_record(req.get("title", ""), "approved",
                               f"заявка {req['id']} подтверждена кнопкой, прогон начат")
    started = time.time()
    try:
        proc = subprocess.run(argv, capture_output=True, text=True,
                              encoding="utf-8", errors="replace")
    except Exception as exc:
        # Запуск не состоялся вовсе. Без этого строка оставалась бы approved навсегда
        # (blocker codex review 09.08.2026): отметка уже стоит, а закрывающей записи
        # не будет — исключение уходит в цикл, минуя весь код ниже.
        _queue_record(req.get("title", ""), "failed",
                      f"запуск не состоялся: {type(exc).__name__}: {str(exc)[:200]}")
        _notify(f"⚠ Заявка {req['id']}: запуск не состоялся — "
                f"{type(exc).__name__}: {str(exc)[:200]}")
        return
    took = int(time.time() - started)
    mins = f"{took // 60} мин {took % 60} с" if took >= 60 else f"{took} с"
    out = (proc.stdout or "").strip()

    # Итог человеческим языком, а не обрывком вывода: коды выхода run_task.ps1
    # осмысленные, и ЛПР должен видеть СМЫСЛ, а не число. Хвост вывода —
    # ниже и коротко, для тех случаев, когда важны детали.
    codes = {
        0: "✅ Рой отработал",
        4: "ℹ Рой уже занят другой задачей — эта присоединится к очереди",
        6: "⚠ Не запущено: MCP не зарегистрирован для канонического роя",
        7: "⚠ Не запущено: движок не нашёл Claude Code (тихая деградация)",
        8: "⚠ Не запущено: истекла авторизация Claude Code — нужен claude auth login",
        9: "⚠ Queen-сессия завершилась с ошибкой",
        11: "⚠ Отработало, НО роя не было: ни одного вызова mcp__claude-flow__",
    }
    head = codes.get(proc.returncode, f"⚠ Код выхода {proc.returncode}")
    verdict = ""
    for line in out.splitlines():
        if "Рой работал" in line or "РОЯ НЕ БЫЛО" in line or "SECRET SCAN" in line:
            verdict += "\n" + line.strip()
    # Исход — в ту же строку очереди.
    #
    # Код 4 («рой уже занят другой задачей») исходом НЕ является: задача не
    # отработала и не провалилась, она просто не начиналась. Её надо ВЕРНУТЬ в
    # queued, а не оставить как есть: перед запуском строка уже помечена approved,
    # и «не трогаем состояние» означало бы вечное approved — а `_next_queued`
    # считает любую approved-строку работающим роем и не выдаёт больше ничего.
    # Очередь встала бы навсегда. Найдено codex review 09.08.2026 как blocker;
    # первая редакция этой правки именно так и делала.
    if proc.returncode == 4:
        state_note = _queue_record(
            req.get("title", ""), "queued",
            "возвращена в очередь: рой был занят другой задачей, прогон не начинался"
        ) or state_note
    else:
        closing = "done" if proc.returncode == 0 else "failed"
        state_note = _queue_record(
            req.get("title", ""), closing,
            f"{head}; заняло {mins}; отчёт {req.get('report', '')}") or state_note
    _notify(f"{head} — заявка {req['id']}, заняло {mins}{verdict[:600]}\n\n"
            f"Отчёт: {req.get('report', '')}"
            + (f"\n\n⚠ Очередь: {state_note}" if state_note else ""))
    # После возврата в очередь выдавать нечего: занят тот самый рой, из-за которого
    # прогон и не начался. Выдаст его собственное закрытие.
    if proc.returncode != 4:
        _push_next()


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


def poll_once(chat_id: str) -> int:
    offset = _load_offset()
    resp = _api("getUpdates", {"offset": offset, "timeout": 50, "allowed_updates": ["callback_query"]})
    if not resp.get("ok"):
        time.sleep(5)
        return 0
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
                if handled:
                    # Закрытие задачи выдаёт следующую (`_push_next`), и её кнопку
                    # ловить некому, если выйти прямо сейчас. Выходим, когда ждать
                    # больше нечего: неотвеченных заявок не осталось.
                    pending = [f for f in os.listdir(QUEUE) if f.endswith(".json")
                               and _status_of(os.path.join(QUEUE, f)) == "pending"]
                    if not pending:
                        print("решение получено и обработано", flush=True)
                        return 0
                    print(f"обработано; ждут решения ещё: {len(pending)}", flush=True)
                    deadline = time.time() + minutes * 60
                if deadline and time.time() > deadline:
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
