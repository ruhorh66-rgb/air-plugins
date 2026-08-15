"""Отказ слушателя обязан быть ГРОМКИМ (ERR-2026-000242).

Три случая за двое суток кончились одинаково: ЛПР жмёт кнопку, ничего не происходит,
слушатель при этом жив и молчит. Молчание было не побочным эффектом, а поведением:
`_api` возвращал {"ok": false} на любую ошибку, `poll_once` спал пять секунд и молчал.

Здесь проверяется ровно то, что раньше отсутствовало: причина попадает в журнал сразу,
после порога уходит человеку в Telegram, восстановление называется, и нажатие,
подобранное первым опросом после старта, помечается накопленным.
"""
from __future__ import annotations

import io
import os
import sys
from contextlib import redirect_stdout

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import approve_listener as al  # noqa: E402

n = 0


def ok(label: str, cond: bool) -> None:
    global n
    assert cond, f"ПРОВАЛ: {label}"
    n += 1
    print("ok  ", label)


def drive(responses, cycles):
    """Гоняет poll_once с подменённым каналом. Возвращает (журнал, сказанное человеку)."""
    said = []
    seq = list(responses)
    al._api = lambda method, payload=None, timeout=70: seq.pop(0) if seq else {"ok": False, "error": "конец"}
    al._notify = lambda text, **k: said.append(text)
    al._load_offset = lambda: 1
    al._save_offset = lambda v: None
    al.time.sleep = lambda s: None
    al._fail_streak = 0
    al._cycle = 0
    al._first_poll_done = False
    al._last_ok_at = al.time.time()
    buf = io.StringIO()
    with redirect_stdout(buf):
        for _ in range(cycles):
            al.poll_once("123")
    return buf.getvalue(), said


saved = (al._api, al._notify, al._load_offset, al._save_offset, al.time.sleep)
try:
    # 1. Одна неудача — причина в журнал, человека не дёргаем.
    log, said = drive([{"ok": False, "error": "Conflict: terminated by other getUpdates"}], 1)
    ok("причина неудачи попадает в журнал", "Conflict" in log)
    ok("одна неудача человека не беспокоит", not said)

    # 2. Порог — говорим вслух, и говорим ПОНЯТНО: кнопка сейчас не работает.
    fail = {"ok": False, "error": "Conflict: terminated by other getUpdates"}
    log, said = drive([fail] * 5, 5)
    ok("после порога сказано человеку", len(said) == 1)
    ok("сказано, что кнопка не работает", "НЕ работает" in said[0])
    ok("названа причина, а не только факт", "Conflict" in said[0])

    # 3. Восстановление называется — иначе в журнале остаётся только паника.
    log, said = drive([fail, fail, {"ok": True, "result": []}], 3)
    ok("восстановление названо", "восстановлен" in log)

    # 4. Признак жизни не зависит от входящих.
    log, said = drive([{"ok": True, "result": []}] * al.HEARTBEAT_EVERY, al.HEARTBEAT_EVERY)
    ok("признак жизни печатается без входящих", "жив, цикл" in log)

    # 5. Накопленное нажатие помечается, а не исполняется молча.
    log, said = drive([{"ok": True, "result": [{"update_id": 5}]}], 1)
    ok("накопленное нажатие названо в журнале", "накопленные нажатия" in log)
    ok("и сказано человеку", any("до запуска слушателя" in s for s in said))

    # 6. Обычный опрос со свежим нажатием таким предупреждением не сопровождается.
    log, said = drive([{"ok": True, "result": []},
                       {"ok": True, "result": [{"update_id": 6}]}], 2)
    ok("свежее нажатие накопленным не объявляется",
       not any("до запуска слушателя" in s for s in said))
finally:
    (al._api, al._notify, al._load_offset, al._save_offset, al.time.sleep) = saved

print(f"\n{n} проверок пройдено")
