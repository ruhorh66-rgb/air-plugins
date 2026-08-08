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
    # Заголовок обрезается: неограниченный title способен раздуть сообщение за
    # лимит Telegram, отправка отвалится, и заявка останется в очереди без
    # кнопки — нерешаемой (codex review 08.08.2026, minor).
    title = (argv[5] if len(argv) > 5 else "Запуск роя")[:120]
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
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as fh:
        json.dump(request, fh, ensure_ascii=False, indent=1)
    os.replace(tmp, path)  # заявка появляется целиком или не появляется вовсе

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

    # Битый или дописываемый прямо сейчас файл в каталоге очереди пропускается,
    # а не роняет create(): иначе заявка уже записана, а кнопка не отправлена —
    # она молча повисает нерешаемой (codex review 08.08.2026, major).
    pending = 0
    try:
        names = os.listdir(QUEUE)
    except OSError:
        names = []
    for name in names:
        if not name.endswith(".json"):
            continue
        try:
            with open(os.path.join(QUEUE, name), encoding="utf-8") as fh:
                d = json.load(fh)
        except (OSError, ValueError):
            continue
        if isinstance(d, dict) and d.get("status") == "pending":
            pending += 1

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
    # Сводка по очереди — решение ЛПР 08.08.2026: одной кнопки мало, когда
    # заявок несколько, надо видеть что ещё висит нерешённым. Идёт ПЕРЕД самой
    # заявкой, чтобы кнопка осталась последним сообщением в чате и не уехала
    # вверх (в первой редакции было наоборот, и комментарий утверждал обратное
    # тому, что делал код — codex review, minor).
    try:
        queue(["--notify"])
    except Exception:
        pass  # сводка не критична — заявка важнее и уйдёт следующей строкой

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
        if not isinstance(d, dict):
            continue  # синтаксически валидный, но не объект — тоже мусор
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
    def fmt(entry) -> str:
        st, rid, title, age, workers = entry
        ago = f"{age} мин назад" if age < 90 else f"{age // 60} ч назад"
        return (f"{marks.get(st, '•')} {(title or rid)[:80]} — {st}, {ago}"
                + (f", воркеров {workers}" if workers else ""))

    pend_lines = [fmt(i) for i in items if i[0] == "pending"]
    done_lines, seen_done = [], 0
    for i in items:
        if i[0] != "pending" and i[3] <= 24 * 60 and seen_done < 5:
            done_lines.append(fmt(i))
            seen_done += 1
    hidden_done = len(items) - len(pend_lines) - len(done_lines)

    # Лимит сообщения Telegram — 4096 символов. Резать хвост вслепую нельзя:
    # первым потеряется нерешённое, ради которого сводка и существует (codex
    # review 08.08.2026, major). Поэтому под нож идут сначала завершённые, а
    # если не хватило и этого — лишние pending, но с явным счётчиком.
    head = "🐝 Очередь заявок на запуск роя\n────────────────\n"
    budget = 3500 - len(head) - 60
    while done_lines and sum(len(x) + 1 for x in pend_lines + done_lines) > budget:
        done_lines.pop()
        hidden_done += 1
    hidden_pend = 0
    while pend_lines and sum(len(x) + 1 for x in pend_lines) > budget:
        pend_lines.pop()
        hidden_pend += 1

    lines = pend_lines + ([f"…и ещё {hidden_pend} нерешённых не поместилось"]
                          if hidden_pend else []) + done_lines
    if hidden_done > 0:
        lines.append(f"…и ещё {hidden_done} завершённых ранее")
    body = (head + ("\n".join(lines) if lines else "пусто")
            + f"\n────────────────\nЖдёт решения: {waiting}")
    if "--notify" in argv:
        _api("sendMessage", {"chat_id": int(_secret("chat_id")), "text": body})
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
