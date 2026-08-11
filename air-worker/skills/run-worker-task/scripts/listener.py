r"""Слушатель air-worker: ловит нажатие кнопки и запускает ОДИН тип задачи.

ЧТО ОН МОЖЕТ И ЧЕГО НЕ МОЖЕТ — это главное в файле. Границы взяты у
`air-ruflo-bridge/.../approve_listener.py` и ослаблены быть не могут: они
условие, при котором канал вообще допустим, а не настройка.

МОЖЕТ: взять заявку с известным id, ПРОВЕРИТЬ ПОДПИСЬ и запустить исполнителя,
выбранного по подписанному полю `task_type`.

НЕ МОЖЕТ, и это не настраивается:
  - выполнить текст из чата. Сообщения не разбираются как команды вообще;
    читается только `callback_data` вида `awrun:<id>` / `awno:<id>`;
  - выполнить что-либо по нажатию от чужого chat_id — сверка строгая, чужие
    нажатия игнорируются молча (ответ подтвердил бы существование канала);
  - выполнить заявку с несошедшейся подписью, просроченную либо повторно;
  - работать параллельно с чужим слушателем ТОГО ЖЕ бота: long polling у
    Telegram эксклюзивен по факту, и второй слушатель просто крадёт апдейты.
    Инцидент 10.08.2026 — кнопка air-worker досталась слушателю ruflo.

Материал задачи в Telegram и в протокол не поднимается: имя файла, хэш, счётчики.
"""
from __future__ import annotations

import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import executors  # noqa: E402
import protocol  # noqa: E402
import worker  # noqa: E402


def _answer(callback_id: str, text: str) -> None:
    worker.api("answerCallbackQuery",
               {"callback_query_id": callback_id, "text": text, "show_alert": True},
               timeout=20)


def _edit(chat_id, message_id: int, text: str) -> None:
    worker.api("editMessageText",
               {"chat_id": int(chat_id), "message_id": message_id, "text": text,
                "reply_markup": {"inline_keyboard": []}}, timeout=20)


def _load_offset() -> int:
    try:
        with open(worker.OFFSET_PATH, encoding="utf-8") as fh:
            return int(fh.read().strip() or 0)
    except (OSError, ValueError):
        return 0


def _save_offset(value: int) -> None:
    try:
        os.makedirs(worker.RUNTIME, exist_ok=True)
        with open(worker.OFFSET_PATH, "w", encoding="utf-8") as fh:
            fh.write(str(value))
    except OSError:
        pass


def run_request(req: dict) -> None:
    """Запустить исполнителя по ПОДПИСАННОМУ типу задачи.

    Строка команды здесь не собирается и собраться не может: `task_type` — ключ
    словаря `executors.REGISTRY`, параметры — подписанный объект, материал —
    подписанный путь с подписанным хэшем.
    """
    spec = executors.REGISTRY.get(req["task_type"])
    if spec is None or not spec["enabled"]:
        worker.set_status(req["id"], "failed", finished_at=time.time(),
                          result=json.dumps({"error": "тип задачи недоступен"},
                                            ensure_ascii=False))
        worker.notify(f"⚠ Заявка {req['id']}: тип {req['task_type']} недоступен, не запускаю")
        return

    os.makedirs(worker.OUT_DIR, exist_ok=True)
    out_path = os.path.join(worker.OUT_DIR, f"{req['id']}.txt")
    worker.set_status(req["id"], "approved", started_at=time.time())
    started = time.time()
    try:
        with protocol.stage("executor_run", external=spec["external"],
                            task_type=req["task_type"],
                            input_sha256=req["input_sha256"][:16]):
            result = spec["run"](req, req["params"], out_path)
    except SystemExit as exc:
        took = int(time.time() - started)
        worker.set_status(req["id"], "failed", finished_at=time.time(),
                          result=json.dumps({"error": str(exc)[:300]}, ensure_ascii=False))
        worker.record_metric(f"air-worker {req['task_type']} {req['id']}",
                             str(req["params"].get("model", "")), "", "", took,
                             "failed", str(exc)[:120])
        worker.notify(f"⚠ Заявка {req['id']} не отработала за {took} с: {str(exc)[:300]}")
        return
    except Exception as exc:  # noqa: BLE001 — слушатель не должен умирать от задачи
        took = int(time.time() - started)
        worker.set_status(req["id"], "failed", finished_at=time.time(),
                          result=json.dumps({"error": f"{type(exc).__name__}: {exc}"[:300]},
                                            ensure_ascii=False))
        worker.record_metric(f"air-worker {req['task_type']} {req['id']}",
                             str(req["params"].get("model", "")), "", "", took,
                             "failed", f"{type(exc).__name__}"[:120])
        worker.notify(f"⚠ Заявка {req['id']}: сбой {type(exc).__name__}: {str(exc)[:200]}")
        return

    took = int(time.time() - started)
    worker.set_status(req["id"], "done", finished_at=time.time(),
                      result=json.dumps(result, ensure_ascii=False))
    worker.record_metric(
        f"air-worker {req['task_type']} {req['id']}", str(result.get("model", "")),
        result.get("tokens_in", ""), result.get("tokens_out", ""), took, "accepted",
        f"chars_in={result.get('chars_in')} chars_out={result.get('chars_out')}")
    mins = f"{took // 60} мин {took % 60} с" if took >= 60 else f"{took} с"
    worker.notify(
        f"✅ Заявка {req['id']} отработала за {mins}\n"
        f"────────────────\n"
        f"Тип: {req['task_type']}, модель: {result.get('model', '—')}\n"
        f"Вход: {result.get('chars_in')} знаков · выход: {result.get('chars_out')} знаков\n"
        f"Токенов: {result.get('tokens_in')} / {result.get('tokens_out')}\n"
        f"Результат: {result.get('output_path')}")


def poll_once(chat_id: str) -> int:
    resp = worker.api("getUpdates",
                      {"offset": _load_offset(), "timeout": 50,
                       "allowed_updates": ["callback_query"]}, timeout=70)
    if not resp.get("ok"):
        time.sleep(5)
        return 0
    handled = 0
    for upd in resp.get("result", []):
        _save_offset(upd["update_id"] + 1)
        cq = upd.get("callback_query")
        if not cq:
            continue
        if str((cq.get("from") or {}).get("id", "")) != str(chat_id):
            continue  # чужой отправитель — молча мимо
        data = str(cq.get("data") or "")
        if ":" not in data:
            continue
        action, rid = data.split(":", 1)
        if action not in ("awrun", "awno"):
            continue  # чужая кнопка (в т.ч. `run:`/`no:` у air-ruflo-bridge) — не наша
        req = worker.load(rid)
        if req is None:
            _answer(cq["id"], "Заявка не найдена")
            continue
        if req["status"] != "pending":
            _answer(cq["id"], f"Уже {req['status']}")
            continue
        msg = cq.get("message") or {}
        mid = msg.get("message_id")
        stamp = time.strftime("%H:%M")
        title = req["title"]

        if worker.is_expired(req):
            worker.set_status(rid, "expired", decided_at=time.time())
            _answer(cq["id"], "Заявка просрочена — подтвердить уже нельзя")
            if mid:
                _edit(chat_id, mid, f"⌛ {title}\n\nЗаявка {rid} просрочена, не запущена.")
            handled += 1
            continue
        if action == "awno":
            worker.set_status(rid, "rejected", decided_at=time.time(), message_id=mid)
            _answer(cq["id"], "Отклонено. Ничего не запускается.")
            if mid:
                _edit(chat_id, mid, f"✖ {title}\n✖ отклонено в {stamp}, заявка {rid}")
            handled += 1
            continue

        # Подпись проверяется В МОМЕНТ НАЖАТИЯ, а не создания: между кнопкой и
        # нажатием проходят часы, и всё это время строку в БД и сам материал можно
        # переписать. Не сошлась — заявка мертва, а не «запустим осторожно».
        ok, why = worker.verify_request(req)
        if not ok:
            worker.set_status(rid, "invalid", decided_at=time.time(),
                              result=json.dumps({"error": why}, ensure_ascii=False))
            _answer(cq["id"], f"Заявка недействительна: {why}")
            if mid:
                _edit(chat_id, mid, f"⛔ {title}\n\nЗаявка {rid} не запущена: {why}")
            worker.notify(f"⛔ Заявка {rid} НЕ запущена — {why}")
            handled += 1
            continue

        _answer(cq["id"], "Принято. Запускаю — итог придёт сюда же.")
        if mid:
            _edit(chat_id, mid, f"⚙ {title}\n✅ подтверждено в {stamp}, заявка {rid}")
        worker.set_status(rid, "approved", decided_at=time.time(), message_id=mid)
        handled += 1
        run_request(req)
    return handled


def main() -> int:
    """Без аргументов — постоянный слушатель. `--once <минуты>` — разовый.

    Разовый режим — предпочтение ЛПР по air-ruflo-bridge: постоянный процесс ради
    кнопки, нажимаемой несколько раз в день, лишняя сущность в контуре.
    """
    foreign, why = worker.foreign_listener_state()
    if foreign != "free":
        print(f"не слушаю: канал бота {foreign} — {why}", file=sys.stderr)
        return 4  # unknown сюда попадает НАМЕРЕННО: непроверенный канал не свободен
    got, note = worker.acquire(note="listener")
    if not got:
        print(f"не слушаю: свой замок {note}", file=sys.stderr)
        return 4
    chat_id = worker.secret("chat_id")
    once = "--once" in sys.argv
    minutes = 30
    if once:
        idx = sys.argv.index("--once")
        minutes = int(sys.argv[idx + 1]) if len(sys.argv) > idx + 1 else 30
        print(f"разовый обработчик: жду решения {minutes} мин", flush=True)
    else:
        print(f"слушатель запущен, база: {worker.DB_PATH}", flush=True)
    deadline = time.time() + minutes * 60 if once else None
    try:
        while True:
            try:
                handled = poll_once(chat_id)
            except KeyboardInterrupt:
                print("остановлен", flush=True)
                return 0
            except Exception as exc:  # noqa: BLE001 — обработчик не должен умирать
                print(f"сбой цикла: {type(exc).__name__}: {exc}", flush=True)
                time.sleep(10)
                continue
            if once:
                if handled:
                    print("решение получено и обработано", flush=True)
                    return 0
                if deadline and time.time() > deadline:
                    print("время ожидания истекло, решения не было", flush=True)
                    return 3
    finally:
        worker.release()


if __name__ == "__main__":
    sys.exit(main())
