"""Слушатель ОБЯЗАН звать проверку подписи перед запуском, а не только иметь её рядом.

Отдельный файл, а не случай в test_request_signature.py, намеренно. Тот проверяет
САМУ функцию проверки, и она всегда была права: подделанную заявку и переписанную цель
она ловила с первого дня. Не проверялось другое — что слушатель её ВЫЗЫВАЕТ. Ровно на
этом зазоре 14.08.2026 прогон TASK-OBS-0055 ушёл по тексту, отличному от подтверждённого
(ERR-2026-000237): функция есть, вызова нет, тесты зелёные.

Поэтому здесь запускается настоящий `_run_request`, а подменяется только то, что
реально выходит за пределы процесса: запуск PowerShell, отправка в Telegram и запись
в очередь. Подмена самой проверки сделала бы тест бессмысленным — он повторил бы
исходную ошибку.
"""
from __future__ import annotations

import hashlib
import json
import os
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import approve_listener as al  # noqa: E402
import approve_via_telegram as avt  # noqa: E402

n = 0


def ok(label: str, cond: bool) -> None:
    global n
    assert cond, f"ПРОВАЛ: {label}"
    n += 1
    print("ok  ", label)


def _request(tmp: str) -> dict:
    """Настоящая подписанная заявка на настоящие пути."""
    objective = os.path.join(tmp, "objective.md")
    with open(objective, "w", encoding="utf-8") as fh:
        fh.write("# цель\n\nтекст, который подтверждает человек\n")
    report = os.path.join(tmp, "report.md")
    req = {
        "id": "test0001",
        "title": "TASK-OBS-9999 (очередь роя)",
        "report": report,
        "objective_file": objective,
        "objective_sha256": hashlib.sha256(
            open(objective, "rb").read()).hexdigest(),
        "target_path": tmp,
        "workers": 3,
        "priority": "high",
        "created_at": 0.0,
        "status": "approved",
    }
    req["sig"] = avt.sign_request(req)
    return req


def run_case(mutate) -> tuple[bool, list[str]]:
    """Возвращает (был ли запуск, что сказано человеку)."""
    launched: list[bool] = []
    said: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        req = _request(tmp)
        mutate(req)

        class _Proc:
            returncode = 0
            stdout = "EXECUTED."
            stderr = ""

        saved = (al.subprocess.run, al._notify, al._queue_record, al._hive_busy)
        al.subprocess.run = lambda *a, **k: (launched.append(True), _Proc())[1]
        al._notify = lambda text, **k: said.append(text)
        al._queue_record = lambda *a, **k: ""
        al._hive_busy = lambda: False
        try:
            al._run_request(req)
        finally:
            (al.subprocess.run, al._notify, al._queue_record,
             al._hive_busy) = saved
    return bool(launched), said


# 1. Нетронутая заявка обязана запускаться — иначе проверка просто ломает канал.
launched, said = run_case(lambda r: None)
ok("нетронутая заявка запускается", launched)

# 2. Цель переписана после подписи — ровно случай 14.08.2026.
def _rewrite(req: dict) -> None:
    with open(req["objective_file"], "a", encoding="utf-8") as fh:
        fh.write("\nстрока, дописанная после нажатия\n")


launched, said = run_case(_rewrite)
ok("переписанная цель НЕ запускается", not launched)
ok("человеку названа причина, а не молчание",
   any("цел" in s.lower() for s in said))

# 3. Подделано подписанное поле.
launched, said = run_case(lambda r: r.update({"workers": 30}))
ok("подделанное поле НЕ запускается", not launched)

# 4. Заявка без подписи.
launched, said = run_case(lambda r: r.pop("sig"))
ok("заявка без подписи НЕ запускается", not launched)

# 5. Проверка сама упала — это отказ, а не разрешение.
saved_verify = avt.verify_request
try:
    def _boom(_req):
        raise RuntimeError("keyring недоступен")

    avt.verify_request = _boom
    sys.modules["approve_via_telegram"].verify_request = _boom
    launched, said = run_case(lambda r: None)
    ok("непроверенное не превращается в разрешённое", not launched)
finally:
    avt.verify_request = saved_verify
    sys.modules["approve_via_telegram"].verify_request = saved_verify

print(f"\n{n} проверок пройдено")
