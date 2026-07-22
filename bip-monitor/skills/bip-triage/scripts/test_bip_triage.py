#!/usr/bin/env python3
"""Проверки скилла bip-triage. Запуск: python test_bip_triage.py

Без фреймворков и без живого BiP: проверяется логика, которая может тихо сломаться —
приватность store, применение правил и дедупликация записей журнала.
"""
import json
import os
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
sys.stdout.reconfigure(encoding="utf-8")


def with_temp_state(fn):
    """Каждый тест — на своей базе, чтобы не трогать реальный store ЛПР."""
    def wrapper():
        # ignore_cleanup_errors: на Windows sqlite держит файл до сборки мусора,
        # иначе тест падает на уборке каталога, а не на проверке.
        with tempfile.TemporaryDirectory(ignore_cleanup_errors=True) as d:
            os.environ["BIP_TRIAGE_STATE_DIR"] = d
            for mod in ("learning_store",):
                sys.modules.pop(mod, None)
            fn()
    wrapper.__name__ = fn.__name__
    return wrapper


@with_temp_state
def test_store_keeps_no_personal_data():
    import learning_store as ls
    db = ls.connect(); ls.create_core(db, ls.SKILL_ID, ls.SCHEMA_VERSION)

    payload = {
        "locator": {"jid": "79990000000@tims.turkcell.com.tr", "time": "19:08"},
        "platform": "claude",
        "initial": {"срочность": "низкая"},
        "correction": {"срочность": "высокая"},
        "scope": {"type": "chat", "value": "79990000000@tims.turkcell.com.tr"},
        "pattern": {"title": "чат прораба — всегда высокий приоритет",
                    "expected": {"срочность": "высокая"}},
    }
    p = Path(os.environ["BIP_TRIAGE_STATE_DIR"]) / "fb.json"
    p.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")

    class A: input = str(p)
    assert ls.cmd_record_feedback(A()) == 0

    blob = ls.db_path().read_bytes().decode("utf-8", "ignore")
    assert "79990000000" not in blob, "номер не должен попадать в базу"
    assert "turkcell" not in blob, "jid не должен попадать в базу"


@with_temp_state
def test_pattern_applies_to_matching_chat_only():
    import learning_store as ls
    db = ls.connect(); ls.create_core(db, ls.SKILL_ID, ls.SCHEMA_VERSION)
    jid = "79990000000@x"
    db.execute(
        "INSERT INTO patterns(pattern_id, status, scope_type, scope_value, trigger_json,"
        " expected_json, forbidden_actions_json, created_at, updated_at)"
        " VALUES('BIP-PAT-000001','active_verified','chat',?,'{}',?,'[]',?,?)",
        (ls.scope_value_hash("chat", jid), json.dumps({"срочность": "высокая"}), ls.now(), ls.now()))
    db.commit()

    hit, fired = ls.apply_patterns({"jid": jid, "text": "привет"},
                                   {"срочность": "низкая", "тема": "т"}, db)
    assert hit["срочность"] == "высокая" and fired == ["BIP-PAT-000001"], "правило должно сработать"
    assert hit["тема"] == "т", "правило не должно затирать поля, которых не задаёт"

    miss, fired2 = ls.apply_patterns({"jid": "79990000999@x", "text": "привет"},
                                     {"срочность": "низкая"}, db)
    assert miss["срочность"] == "низкая" and not fired2, "чужой чат не должен задевать правило"


@with_temp_state
def test_keyword_scope_matches_text():
    import learning_store as ls
    db = ls.connect(); ls.create_core(db, ls.SKILL_ID, ls.SCHEMA_VERSION)
    db.execute(
        "INSERT INTO patterns(pattern_id, status, scope_type, scope_value, trigger_json,"
        " expected_json, forbidden_actions_json, created_at, updated_at)"
        " VALUES('BIP-PAT-000002','active_provisional','keyword','авария',?,?,'[]',?,?)",
        ("{}", json.dumps({"срочность": "высокая", "нужен_ответ": True}), ls.now(), ls.now()))
    db.commit()
    hit, fired = ls.apply_patterns({"jid": "a@x", "text": "На объекте АВАРИЯ, нужен ответ"},
                                   {"срочность": "низкая", "нужен_ответ": False}, db)
    assert hit["срочность"] == "высокая" and hit["нужен_ответ"] is True, "ключевое слово сработало"
    miss, _ = ls.apply_patterns({"jid": "a@x", "text": "всё спокойно"},
                                {"срочность": "низкая"}, db)
    assert miss["срочность"] == "низкая", "без ключевого слова правило не применяется"


@with_temp_state
def test_retire_keeps_the_rule_and_its_reason():
    import learning_store as ls
    db = ls.connect(); ls.create_core(db, ls.SKILL_ID, ls.SCHEMA_VERSION)
    db.execute(
        "INSERT INTO patterns(pattern_id, status, scope_type, scope_value, trigger_json,"
        " expected_json, forbidden_actions_json, created_at, updated_at)"
        " VALUES('BIP-PAT-000003','active_verified','global','','{}','{}','[]',?,?)",
        (ls.now(), ls.now()))
    db.commit()

    class A: pattern_id = "BIP-PAT-000003"; reason = "чат закрыт"
    assert ls.cmd_retire(A()) == 0
    row = db.execute("SELECT status, retire_reason FROM patterns"
                     " WHERE pattern_id='BIP-PAT-000003'").fetchone()
    assert row["status"] == "retired" and row["retire_reason"] == "чат закрыт", \
        "правило не удаляется, а помечается недействующим с причиной"
    assert not ls.apply_patterns({"jid": "a@x", "text": "t"}, {"срочность": "низкая"}, db)[1], \
        "снятое правило больше не применяется"


if __name__ == "__main__":
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    for t in tests:
        t()
        print(f"OK  {t.__name__}")
    print(f"\n{len(tests)} проверок пройдено")
