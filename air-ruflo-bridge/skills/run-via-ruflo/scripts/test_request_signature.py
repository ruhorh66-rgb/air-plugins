"""Подпись заявки: подмена полей после создания обязана ломать проверку.

Запуск: python test_request_signature.py

ЗАЧЕМ ОТДЕЛЬНЫМ ФАЙЛОМ, А НЕ В test_approve_queue.py. Тот файл сейчас правит рой
по TASK-OBS-0045 — писать в него одновременно значит потерять чьи-то изменения.
Когда прогон закроется, объединить можно; расходиться по смыслу этим двум наборам
не из-за чего.

ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ. Заявка читается слушателем в момент нажатия, а не создания.
Между кнопкой и нажатием проходят часы, и на каталоге очереди `Everyone: FullControl`
(замер 10.08.2026). Значит подмена цели после того, как ЛПР прочитал сводку, —
доступное действие, и единственное, что от него защищает, — подпись.
"""
from __future__ import annotations

import json
import os
import sys
import tempfile

sys.stdout.reconfigure(encoding="utf-8")
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import approve_via_telegram as m  # noqa: E402

# Настоящий файл цели: часть проверок трогает его содержимое, и подменять его
# приходится по-настоящему — на выдуманном пути этого не проверить.
_TMP = tempfile.mkdtemp(prefix="sigtest-")
OBJECTIVE = os.path.join(_TMP, "задание.md")
with open(OBJECTIVE, "w", encoding="utf-8") as _fh:
    _fh.write("# Задание\n\nСделать ровно это и ничего другого.\n")

BASE = {
    "id": "sigtest1",
    "title": "TASK-OBS-0099 (очередь роя)",
    "report": r"E:\-4-\ruflo-hive\test.md",
    "objective_file": OBJECTIVE,
    "objective_sha256": m.file_digest(OBJECTIVE),
    "target_path": r"E:\-7-",
    "workers": 5,
    "priority": "high",
    "created_at": 1786370000.0,
    "status": "pending",
}


def _signed() -> dict:
    req = dict(BASE)
    req["sig"] = m.sign_request(req)
    return req


def test_signature_holds_for_untouched_request():
    ok, why = m.verify_request(_signed())
    assert ok, why


def test_tampering_any_signed_field_breaks_it():
    """Именно ради этого всё и написано: подмена цели или каталога после сводки."""
    for field, value in (("objective_file", r"E:\-5-\чужое задание.md"),
                         ("target_path", r"E:\-8-"),
                         ("report", r"E:\-4-\чужой.md"),
                         ("workers", 32),
                         ("priority", "critical"),
                         ("title", "TASK-OBS-0001 (очередь роя)"),
                         ("id", "подменён"),
                         ("created_at", 1786000000.0)):
        bad = _signed()
        bad[field] = value
        ok, why = m.verify_request(bad)
        assert not ok, f"подмена {field} прошла проверку"
        assert "не сходится" in why, why


def test_listener_own_fields_do_not_break_signature():
    """Слушатель дописывает решение в тот же файл. Если бы это ломало подпись,
    механизм отвергал бы собственную запись и не работал бы вовсе."""
    req = _signed()
    req.update({"status": "approved", "decided_at": 1786370099.0, "message_id": 42})
    ok, why = m.verify_request(req)
    assert ok, why


def test_unsigned_request_is_refused():
    """«Проверить не смог» в вопросе исполнения равносильно отказу. Заявка без
    подписи — это старая заявка либо подложенная; исполнять нельзя ни ту, ни другую."""
    req = dict(BASE)
    ok, why = m.verify_request(req)
    assert not ok and "без подписи" in why, why


def test_field_order_does_not_matter():
    """Файл перезаписывается слушателем, порядок ключей после json.load не гарантирован.
    Подпись, зависящая от порядка, разваливалась бы сама по себе."""
    req = _signed()
    shuffled = {k: req[k] for k in sorted(req, reverse=True)}
    ok, why = m.verify_request(shuffled)
    assert ok, why


def test_rewriting_the_objective_file_is_caught():
    """ГЛАВНАЯ проверка. Подписать путь и не подписать содержимое — закрыть половину
    дыры: файл читается заново уже ПОСЛЕ подтверждения. ЛПР увидел бы сводку одного
    задания, а исполнилось бы другое, причём подпись сошлась бы."""
    req = _signed()
    ok, _ = m.verify_request(req)
    assert ok
    original = open(OBJECTIVE, encoding="utf-8").read()
    try:
        with open(OBJECTIVE, "w", encoding="utf-8") as fh:
            fh.write("# Задание\n\nА теперь сделать совсем другое.\n")
        ok, why = m.verify_request(req)
        assert not ok, "подмена содержимого цели прошла проверку"
        assert "изменил" in why, why
    finally:
        with open(OBJECTIVE, "w", encoding="utf-8") as fh:
            fh.write(original)
    assert m.verify_request(req)[0], "возврат исходного текста должен снова сходиться"


def test_missing_objective_file_is_refused():
    """Цель исчезла между кнопкой и нажатием — исполнять нечего и незачем."""
    req = _signed()
    gone = dict(req)
    gone["objective_file"] = os.path.join(_TMP, "нет-такого.md")
    ok, _ = m.verify_request(gone)
    assert not ok, "подмена пути должна ломать подпись"


def test_survives_json_round_trip():
    """Заявка живёт файлом: подпись обязана сходиться после записи и чтения.
    Числа при сериализации меняют представление (5 против 5.0), и канонизация,
    чувствительная к этому, отвергала бы правильные заявки — fail-closed, но
    механизм переставал бы работать (codex review 10.08.2026, low)."""
    req = _signed()
    path = os.path.join(_TMP, "req.json")
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(req, fh, ensure_ascii=False, indent=1)
    with open(path, encoding="utf-8") as fh:
        back = json.load(fh)
    ok, why = m.verify_request(back)
    assert ok, f"после round-trip подпись развалилась: {why}"


if __name__ == "__main__":
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    for t in tests:
        t()
        print(f"ok  {t.__name__}")
    print(f"\n{len(tests)} проверок пройдено")
