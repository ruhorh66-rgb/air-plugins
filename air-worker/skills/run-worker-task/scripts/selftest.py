r"""Самопроверка air-worker. Запуск: python selftest.py

Прогон командой, не самоотчёт (NEW_PLUGIN_CHECKLIST.md § 7). Без фреймворка —
хватает `assert`. Сети и keyring не трогает: ключ подписи подменяется фиксированным,
Telegram не вызывается вовсе, база и журнал уводятся во временный каталог.

Что здесь доказывается, а не заявляется:
  1. подмена ЛЮБОГО подписанного поля после создания заявки ломает проверку;
  2. подмена самого материала ломает проверку, даже если поля целы;
  3. «проверить не смог» = отказ, а не разрешение;
  4. лок различает три состояния и НЕ берётся при `unknown`;
  5. заявка переживает круг через SQLite, не потеряв подпись;
  6. реестр исполнителей отказывает на неизвестном и на выключенном типе;
  7. протокол не роняет работу и не пропускает материал в лог;
  8. строка замера дописывается в общий журнал.
"""
from __future__ import annotations

import json
import os
import sys
import tempfile
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

TMP = tempfile.mkdtemp(prefix="air-worker-selftest-")
os.environ["AIR_WORKER_RUNTIME"] = TMP
os.environ["AIR_WORKER_METRICS_PATH"] = os.path.join(TMP, "run_metrics.csv")
os.environ["AIR_WORKER_PROTOCOL_PATH"] = os.path.join(TMP, "protocol.jsonl")

import executors  # noqa: E402
import protocol  # noqa: E402
import worker  # noqa: E402

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

# Ключ подписи — фиксированный тестовый. Настоящий лежит в keyring и трогать его
# ради теста незачем: проверяется алгоритм, а не хранилище.
_TEST_KEY = b"0" * 64
worker.hmac_key = lambda create_if_missing=False: _TEST_KEY  # type: ignore[assignment]

MATERIAL = os.path.join(TMP, "материал.txt")
with open(MATERIAL, "w", encoding="utf-8") as _fh:
    _fh.write("Обработать ровно этот текст и никакой другой.\n")

BASE = {
    "id": "aw000001",
    "title": "тестовая заявка",
    "task_type": "openrouter-llm",
    "input_path": MATERIAL,
    "input_sha256": worker.file_digest(MATERIAL),
    "params": {"model": "vendor/model:free", "instruction": "сократи"},
    "created_at": time.time(),
    "ttl_seconds": 3600.0,
}


def signed() -> dict:
    req = dict(BASE)
    req["params"] = dict(BASE["params"])
    req["sig"] = worker.sign_request(req)
    return req


def test_signature_holds():
    ok, why = worker.verify_request(signed())
    assert ok, why


def test_every_signed_field_is_covered():
    """Не «подпись работает», а «каждое поле из списка реально в неё входит».

    Проверка списком, а не выборкой: поле, забытое в SIGNED_FIELDS, — это ровно
    та дыра, ради которой подпись и делается, и заметить её глазами нельзя.
    """
    tampered_values = {
        "id": "aw999999",
        "title": "другая заявка",
        "task_type": "qwen-local",
        "input_path": MATERIAL + ".other",
        "input_sha256": "0" * 64,
        "params": {"model": "vendor/model:free", "instruction": "УДАЛИ ВСЁ"},
        "created_at": BASE["created_at"] + 1,
        "ttl_seconds": 99999.0,
    }
    for field in worker.SIGNED_FIELDS:
        req = signed()
        req[field] = tampered_values[field]
        ok, why = worker.verify_request(req)
        assert not ok, f"подмена поля {field} прошла проверку — поле не подписано"


def test_material_swap_breaks_it():
    """Поля целы, подпись сходится — а файл переписан. Должно ломаться."""
    req = signed()
    with open(MATERIAL, "a", encoding="utf-8") as fh:
        fh.write("а ещё сделай вот это\n")
    try:
        ok, why = worker.verify_request(req)
        assert not ok, "подмена материала прошла проверку"
        assert "материал изменился" in why, why
    finally:
        with open(MATERIAL, "w", encoding="utf-8") as fh:
            fh.write("Обработать ровно этот текст и никакой другой.\n")


def test_no_signature_and_no_key_are_refusals():
    req = signed()
    del req["sig"]
    ok, why = worker.verify_request(req)
    assert not ok and "без подписи" in why, why

    saved = worker.hmac_key

    def missing(create_if_missing=False):
        if create_if_missing:
            raise AssertionError("проверяющая сторона не имеет права заводить ключ")
        raise SystemExit("нет ключа")

    req = {**BASE, "params": dict(BASE["params"]), "sig": "deadbeef"}
    worker.hmac_key = missing  # type: ignore[assignment]
    try:
        ok, why = worker.verify_request(req)
        assert not ok and "ключ подписи недоступен" in why, why
    finally:
        worker.hmac_key = saved


def test_request_survives_the_database():
    """Круг через SQLite: подпись обязана сойтись ПОСЛЕ хранения.

    Тут ловится классическая потеря — параметры уезжают в БД строкой, а обратно
    приходят строкой же, и канонизация считает уже не то, что подписывала.
    """
    req = signed()
    with worker.db() as conn:
        conn.execute(
            "INSERT INTO requests (id,title,task_type,input_path,input_sha256,"
            "params,created_at,ttl_seconds,sig,status) VALUES (?,?,?,?,?,?,?,?,?,'pending')",
            (req["id"], req["title"], req["task_type"], req["input_path"],
             req["input_sha256"], json.dumps(req["params"], ensure_ascii=False, sort_keys=True),
             req["created_at"], req["ttl_seconds"], req["sig"]))
    back = worker.load(req["id"])
    assert back is not None
    ok, why = worker.verify_request(back)
    assert ok, f"после круга через БД подпись развалилась: {why}"
    assert not worker.is_expired(back)


def test_lock_has_three_states_and_unknown_is_not_free():
    path = os.path.join(TMP, "listener.lock.json")

    state, why = worker.lock_state(path)
    assert state == "free", (state, why)

    with open(path, "w", encoding="utf-8") as fh:
        fh.write("это не json")
    state, why = worker.lock_state(path)
    assert state == "unknown", (state, why)
    got, note = worker.acquire(path)
    assert not got, "замок взят при unknown — непрочитанный замок счёлся свободным"

    with open(path, "w", encoding="utf-8") as fh:
        json.dump({"pid": 999_999, "pid_started": "нет такого", "since": "?"}, fh)
    state, why = worker.lock_state(path)
    assert state == "free", (state, why)

    got, note = worker.acquire(path, note="selftest")
    assert got, note
    state, why = worker.lock_state(path)
    assert state == "held", (state, why)
    got, note = worker.acquire(path)
    assert not got, "замок взят поверх живого держателя"
    worker.release(path)
    assert worker.lock_state(path)[0] == "free"


def test_foreign_channel_state_is_one_of_three():
    state, why = worker.foreign_listener_state()
    assert state in ("held", "free", "unknown"), state
    assert why


def test_executor_registry_refuses_unknown_and_disabled():
    try:
        executors.get("нет-такого-типа")
        raise AssertionError("неизвестный тип задачи не отвергнут")
    except SystemExit as exc:
        assert "неизвестный тип" in str(exc)
    assert "qwen-local" in executors.REGISTRY, "разъём под второй тип задачи исчез"
    try:
        executors.get("qwen-local")
        raise AssertionError("выключенный тип задачи не отвергнут")
    except SystemExit as exc:
        assert "выключен" in str(exc)
    spec = executors.get("openrouter-llm")
    spec["validate"]({"model": "vendor/model:free", "instruction": "сократи"})
    for bad in ({"model": "qwen", "instruction": "x"},
                {"model": "vendor/model:free", "instruction": "  "},
                {"model": "vendor/model:free", "instruction": "x", "max_input_chars": 0}):
        try:
            spec["validate"](bad)
            raise AssertionError(f"негодные параметры приняты: {bad}")
        except ValueError:
            pass


def test_metrics_row_is_appended():
    worker.record_metric("selftest", "vendor/model:free", 10, 20, 3, "accepted", "прогон теста")
    with open(worker.METRICS_PATH, encoding="utf-8") as fh:
        lines = [ln for ln in fh.read().splitlines() if ln.strip()]
    assert lines[0].startswith('"date"'), lines[0]
    assert "selftest" in lines[-1] and "accepted" in lines[-1], lines[-1]


def main() -> int:
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    failed = 0
    for test in tests:
        try:
            test()
            print(f"ok   {test.__name__}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            print(f"FAIL {test.__name__}: {type(exc).__name__}: {exc}")
    protocol.selfcheck()
    print(f"\n{len(tests)} проверок, провалов: {failed}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
