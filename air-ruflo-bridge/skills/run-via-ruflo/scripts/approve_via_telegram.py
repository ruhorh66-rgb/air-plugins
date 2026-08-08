r"""Подтверждение запуска роя кнопкой в Telegram вместо возврата к консоли.

ЗАЧЕМ. Гейт человека перед реальным запуском роя обязателен и не обсуждается.
Но до сих пор он требовал, чтобы ЛПР физически сел за консоль SRVLM01 и набрал
команду с -Approval: классификатор Claude Code не даёт сессии запустить
автономного агента самой, и это правильно. На практике ЛПР часто работает с
планшета, и «сбегать к консоли» съедало больше времени, чем сама постановка
задачи.

ЧТО МЕНЯЕТСЯ И ЧТО НЕТ. Меняется КАНАЛ подтверждения: вместо набора команды —
нажатие кнопки в Telegram. НЕ меняется само право: каждый запуск по-прежнему
подтверждает лично ЛПР, поимённо для конкретной заявки. Это не обход гейта и не
autoMode.allow (тот снял бы блок классификатора вообще, на все автономные
запуски — ЛПР сознательно отказался, прошлый опыт был плохим).

ГРАНИЦЫ БЕЗОПАСНОСТИ — почему бот не становится дырой в машину:

  1. Слушатель НЕ исполняет команды из Telegram. Он умеет ровно одно — запустить
     заявку, которую УЖЕ подготовил dry-run на этой машине. Текст из чата
     командой не становится ни при каких условиях.
  2. Принимается нажатие только с сохранённого chat_id ЛПР; любой другой
     отправитель игнорируется молча (не отвечаем, чтобы не подтверждать чужому
     существование канала).
  3. Заявка одноразовая и с TTL: после запуска и по истечении срока она
     непригодна, повторное нажатие ничего не делает.
  4. Токен и chat_id берутся из Windows keyring, не из файлов и не из git.

Заявка кладётся в приватный рантайм (E:\-4-\skill-state\ruflo-approvals), не в
репозиторий: это операционное состояние, ему в git не место.
"""
from __future__ import annotations

import json
import os
import secrets
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

try:
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
except Exception:
    pass

SERVICE = "air-comms-telegram-bot"
QUEUE = r"E:\-4-\skill-state\ruflo-approvals"
TTL_SECONDS = 12 * 3600


def _secret(account: str) -> str:
    import keyring
    value = keyring.get_password(SERVICE, account)
    if not value:
        raise SystemExit(f"нет учётной записи {SERVICE}/{account} в keyring")
    return value


def _api(method: str, payload: dict) -> dict:
    token = _secret("botfather-token")
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        f"https://api.telegram.org/bot{token}/{method}",
        data=data, headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


def create(argv: list[str]) -> int:
    """Положить заявку в очередь и прислать ЛПР кнопку.

    ЗАЯВКА ХРАНИТ ПАРАМЕТРЫ, А НЕ КОМАНДУ — переделано 08.08.2026 после того,
    как классификатор Claude Code справедливо заблокировал первую версию.
    В той версии сессия писала в заявку готовую строку запуска с -Approval,
    то есть протаскивала обход гейта через файл: кто угодно, получив запись в
    очередь, исполнил бы произвольную команду на машине.

    Теперь протащить туда нечего: слушатель сам собирает вызов run_task.ps1 из
    фиксированного шаблона, подставляя только цель, каталог и числа. Команды
    как строки в этом канале не существует.
    """
    if len(argv) < 3:
        print("usage: create <report> <objective_file> <target_path> "
              "[workers] [priority] [заголовок]", file=sys.stderr)
        return 2
    report, objective_file, target = argv[0], argv[1], argv[2]
    workers = argv[3] if len(argv) > 3 else "5"
    priority = argv[4] if len(argv) > 4 else "high"
    title = argv[5] if len(argv) > 5 else "Запуск роя"
    if not os.path.isfile(objective_file):
        raise SystemExit(f"нет файла цели: {objective_file}")
    if not os.path.isdir(target):
        raise SystemExit(f"нет каталога задачи: {target}")
    if not str(workers).isdigit() or not (1 <= int(workers) <= 32):
        raise SystemExit("workers: целое 1..32")
    if priority not in ("normal", "high", "critical"):
        raise SystemExit("priority: normal|high|critical")

    os.makedirs(QUEUE, exist_ok=True)
    rid = secrets.token_hex(4)
    request = {
        "id": rid,
        "title": title,
        "report": report,
        "objective_file": os.path.abspath(objective_file),
        "target_path": os.path.abspath(target),
        "workers": int(workers),
        "priority": priority,
        "created_at": time.time(),
        "status": "pending",
    }
    path = os.path.join(QUEUE, f"{rid}.json")
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(request, fh, ensure_ascii=False, indent=1)

    chat_id = _secret("chat_id")

    # Первые строки файла цели — краткая суть задачи. ЛПР работает с очередью
    # задач на вайб-кодинг и через несколько дней не помнит, что за заявка:
    # «Запуск роя, заявка a1b2» ни о чём не говорит. Поэтому в сообщение идёт
    # выжимка из самой цели, а не только заголовок.
    gist = ""
    try:
        with open(objective_file, encoding="utf-8") as fh:
            lines = [ln.strip() for ln in fh.read().splitlines() if ln.strip()]
        gist = " ".join(lines[:2])[:300]
    except OSError:
        pass

    pending = 0
    try:
        for name in os.listdir(QUEUE):
            if not name.endswith(".json"):
                continue
            with open(os.path.join(QUEUE, name), encoding="utf-8") as fh:
                if json.load(fh).get("status") == "pending":
                    pending += 1
    except OSError:
        pass

    queue_line = f"\nВ очереди ждёт решения: {pending}" if pending > 1 else ""
    text = (f"🐝 {title}\n"
            f"────────────────\n"
            f"ЧТО ДЕЛАЕМ: {gist or '(цель — в файле)'}\n\n"
            f"Каталог: {os.path.basename(target)}\n"
            f"Воркеров: {workers} · приоритет: {priority}\n"
            f"Цель целиком: {objective_file}\n"
            f"Отчёт dry-run: {report}\n"
            f"────────────────\n"
            f"Заявка {rid}, кнопка действует {TTL_SECONDS // 3600} ч.{queue_line}")
    resp = _api("sendMessage", {
        "chat_id": int(chat_id),
        "text": text,
        "reply_markup": {"inline_keyboard": [[
            {"text": "✅ Запустить", "callback_data": f"run:{rid}"},
            {"text": "✖ Отклонить", "callback_data": f"no:{rid}"},
        ]]},
    })
    if not resp.get("ok"):
        raise SystemExit("Telegram отклонил отправку заявки")

    # Сводка по очереди сразу за заявкой — решение ЛПР 08.08.2026. Когда заявок
    # несколько, одной кнопки мало: надо видеть, что ещё висит нерешённым и что
    # уже отработало, не спрашивая отдельно. Сводка идёт ПОСЛЕ самой заявки,
    # чтобы кнопка оставалась последним сообщением и не уезжала вверх.
    try:
        queue(["--notify"])
    except Exception:
        pass  # сводка не критична — заявка уже отправлена и работает

    print(json.dumps({"request_id": rid, "queued": path, "pending": pending},
                     ensure_ascii=False))
    return 0


def status(argv: list[str]) -> int:
    """Состояние заявки: pending / approved / rejected / expired."""
    if not argv:
        print("usage: status <request_id>", file=sys.stderr)
        return 2
    path = os.path.join(QUEUE, f"{argv[0]}.json")
    if not os.path.isfile(path):
        print(json.dumps({"status": "unknown"}))
        return 1
    with open(path, encoding="utf-8") as fh:
        data = json.load(fh)
    if data.get("status") == "pending" and time.time() - data.get("created_at", 0) > TTL_SECONDS:
        data["status"] = "expired"
    print(json.dumps({k: data.get(k) for k in ("id", "status", "title", "decided_at")},
                     ensure_ascii=False))
    return 0


def queue(argv: list[str]) -> int:
    """Очередь заявок: что ждёт решения, что уже отработано.

    Нужна с переходом к очереди задач на вайб-кодинг: заявок становится
    несколько, и без общего вида непонятно, что висит, а что закрыто.
    С `--notify` сводка уходит в Telegram, иначе печатается локально.
    """
    items = []
    try:
        names = sorted(os.listdir(QUEUE))
    except OSError:
        names = []
    now = time.time()
    for name in names:
        if not name.endswith(".json"):
            continue
        try:
            with open(os.path.join(QUEUE, name), encoding="utf-8") as fh:
                d = json.load(fh)
        except (OSError, ValueError):
            continue
        st = d.get("status", "?")
        if st == "pending" and now - d.get("created_at", 0) > TTL_SECONDS:
            st = "expired"
        age = int((now - d.get("created_at", 0)) / 60)
        items.append((st, d.get("id", "?"), d.get("title", ""), age, d.get("workers")))

    order = {"pending": 0, "approved": 1, "rejected": 2, "expired": 3, "cancelled": 4}
    items.sort(key=lambda x: (order.get(x[0], 9), x[3]))
    marks = {"pending": "⏳", "approved": "✅", "rejected": "✖",
             "expired": "⌛", "cancelled": "🧹"}
    waiting = sum(1 for i in items if i[0] == "pending")

    # Нерешённое показываем всё, решённое — только свежее и не больше пяти:
    # сводка должна оставаться читаемой через месяц работы, а не превращаться
    # в архив. Полная история и так лежит в файлах очереди.
    lines, shown_done = [], 0
    for st, rid, title, age, workers in items:
        if st != "pending":
            if age > 24 * 60 or shown_done >= 5:
                continue
            shown_done += 1
        ago = f"{age} мин назад" if age < 90 else f"{age // 60} ч назад"
        lines.append(f"{marks.get(st, '•')} {title or rid} — {st}, {ago}"
                     + (f", воркеров {workers}" if workers else ""))
    hidden = len(items) - len(lines)
    if hidden > 0:
        lines.append(f"…и ещё {hidden} завершённых ранее")
    body = ("🐝 Очередь заявок на запуск роя\n────────────────\n"
            + ("\n".join(lines) if lines else "пусто")
            + f"\n────────────────\nЖдёт решения: {waiting}")
    if "--notify" in argv:
        _api("sendMessage", {"chat_id": int(_secret("chat_id")), "text": body[:3500]})
        print(f"сводка отправлена, ждёт решения: {waiting}")
    else:
        print(body)
    return 0


def main(argv: list[str]) -> int:
    if not argv:
        print(__doc__)
        return 2
    if argv[0] == "create":
        return create(argv[1:])
    if argv[0] == "status":
        return status(argv[1:])
    if argv[0] == "queue":
        return queue(argv[1:])
    print(f"неизвестная команда: {argv[0]}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
