#!/usr/bin/env python3
"""Локальный store обучения для скилла bip-triage.

Реализует общий контракт обучения скиллов (`store_schema.py`, v0.2): паспорт
`learning_meta`, `feedback` (сырые правки ЛПР) и `patterns` (выведенные правила) с теми же
именами полей, что у остальных скиллов — чтобы один анализатор читал базы всех скиллов.

Приватность: НЕ хранит тексты сообщений, имена, номера и jid. Чат и сообщение
адресуются только через HMAC локатора; ключ лежит рядом с базой.

Что здесь специфично для BiP: scope-типы (chat/contact/group/keyword) и применение
правил к результату триажа — `apply`, которым пользуется bip_desktop_monitor.

Использование:
    python learning_store.py init
    python learning_store.py record-feedback --input feedback.json
    python learning_store.py export-context
    python learning_store.py apply --input triage.json
    python learning_store.py confirm-pattern --pattern-id BIP-PAT-000001
    python learning_store.py retire-pattern --pattern-id BIP-PAT-000001 --reason "..."
"""

from __future__ import annotations

import argparse
import hashlib
import hmac
import json
import os
import re
import sqlite3
import sys
from pathlib import Path

from store_schema import (  # вендорится, не форкается — см. шапку store_schema.py
    CONTRACT_VERSION,
    assert_writable,
    create_core,
    log_history,
    now,
)

SCHEMA_VERSION = "1"
SKILL_ID = "bip-triage"
FEEDBACK_PREFIX = "BIP-FBK"
PATTERN_PREFIX = "BIP-PAT"
VERIFIED_THRESHOLD = 3

# chat/contact/group адресуют собеседника (через хеш), keyword — слово в тексте,
# message — разовая правка без обобщения, global — правило для всех чатов.
VALID_SCOPES = {"message", "chat", "contact", "group", "keyword", "project", "global"}

URGENCY = ("низкая", "средняя", "высокая")


def state_dir() -> Path:
    """ПДн-содержащее состояние живёт вне плагина и вне vault (RISK-019)."""
    override = os.environ.get("BIP_TRIAGE_STATE_DIR")
    if override:
        return Path(override)
    local = os.environ.get("LOCALAPPDATA")
    if local:
        return Path(local) / "Tech77" / "BipTriage"
    xdg = os.environ.get("XDG_DATA_HOME")
    return Path(xdg) / "tech77-bip-triage" if xdg else Path.home() / ".tech77-bip-triage"


def db_path() -> Path:
    return state_dir() / "bip_triage.sqlite3"


def _key() -> bytes:
    """HMAC-ключ рядом с базой. Создаётся один раз, права — только пользователю."""
    path = state_dir() / "hmac.key"
    if not path.exists():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(os.urandom(32))
        try:
            os.chmod(path, 0o600)
        except OSError:
            pass  # Windows без POSIX-прав — каталог всё равно пользовательский
    return path.read_bytes()


def subject_ref(locator: dict) -> str:
    """HMAC локатора сообщения/чата. В базу не попадает ни jid, ни текст."""
    raw = json.dumps(locator, sort_keys=True, ensure_ascii=False).encode("utf-8")
    return hmac.new(_key(), raw, hashlib.sha256).hexdigest()


def scope_value_hash(scope_type: str, value: str) -> str:
    """Значение scope тоже хешируется, кроме keyword — его надо сопоставлять по тексту."""
    if scope_type in ("keyword", "global", "project"):
        return value.strip().lower()
    return hmac.new(_key(), value.strip().lower().encode("utf-8"), hashlib.sha256).hexdigest()


def connect() -> sqlite3.Connection:
    state_dir().mkdir(parents=True, exist_ok=True)
    db = sqlite3.connect(db_path())
    db.row_factory = sqlite3.Row
    db.execute("PRAGMA foreign_keys = ON")
    return db


def _next_id(db: sqlite3.Connection, table: str, column: str, prefix: str) -> str:
    row = db.execute(f"SELECT {column} FROM {table} WHERE {column} LIKE ?"
                     f" ORDER BY {column} DESC LIMIT 1", (f"{prefix}-%",)).fetchone()
    n = int(row[0].rsplit("-", 1)[1]) + 1 if row else 1
    return f"{prefix}-{n:06d}"


def cmd_init(_args) -> int:
    db = connect()
    create_core(db, SKILL_ID, SCHEMA_VERSION)
    db.commit()
    print(json.dumps({"db": str(db_path()), "contract": CONTRACT_VERSION,
                      "schema": SCHEMA_VERSION}, ensure_ascii=False))
    return 0


def cmd_record_feedback(args) -> int:
    """Правка ЛПР → эпизод, и (если задан pattern) сразу действующее правило."""
    payload = json.loads(Path(args.input).read_text(encoding="utf-8-sig"))  # utf-8-sig: терпим BOM от Windows-инструментов
    scope = payload.get("scope") or {}
    stype = scope.get("type", "message")
    if stype not in VALID_SCOPES:
        print(f"неизвестный scope: {stype}; допустимы {sorted(VALID_SCOPES)}", file=sys.stderr)
        return 2

    db = connect()
    create_core(db, SKILL_ID, SCHEMA_VERSION)
    ref = subject_ref(payload.get("locator") or {})
    svalue = scope_value_hash(stype, str(scope.get("value", "")))

    fid = _next_id(db, "feedback", "feedback_id", FEEDBACK_PREFIX)
    pid = None
    pattern = payload.get("pattern")
    if pattern:
        pid = _next_id(db, "patterns", "pattern_id", PATTERN_PREFIX)
        body = json.dumps(pattern.get("trigger") or {}, ensure_ascii=False)
        expected = json.dumps(pattern.get("expected") or {}, ensure_ascii=False)
        db.execute(
            "INSERT INTO patterns(pattern_id, status, scope_type, scope_value,"
            " trigger_json, expected_json, forbidden_actions_json, confirmations,"
            " created_at, updated_at, title, description, content, kind, origin,"
            " rule_key, last_confirmed_at)"
            " VALUES(?,?,?,?,?,?,?,1,?,?,?,?,?,?,?,?,?)",
            (pid, "active_provisional", stype, svalue, body, expected,
             json.dumps(pattern.get("forbidden_actions") or [], ensure_ascii=False),
             now(), now(), pattern.get("title"), pattern.get("description"),
             pattern.get("content"), pattern.get("kind", "rule"),
             "human" if payload.get("authored_by_lpr") else "derived",
             pattern.get("rule_key"), now()),
        )
        log_history(db, pid, "ADD", "human", None, expected,
                    pattern.get("description") or "правка ЛПР")

    db.execute(
        "INSERT INTO feedback(feedback_id, subject_ref, platform, initial_json,"
        " correction_json, scope_type, scope_value, pattern_id, created_at)"
        " VALUES(?,?,?,?,?,?,?,?,?)",
        (fid, ref, payload.get("platform", "claude"),
         json.dumps(payload.get("initial") or {}, ensure_ascii=False),
         json.dumps(payload.get("correction") or {}, ensure_ascii=False),
         stype, svalue, pid, now()),
    )
    if pid:
        db.execute("INSERT INTO pattern_evidence(pattern_id, feedback_id, polarity, ts)"
                   " VALUES(?,?,1,?)", (pid, fid, now()))
    db.commit()
    print(json.dumps({"feedback_id": fid, "pattern_id": pid}, ensure_ascii=False))
    return 0


def active_patterns(db: sqlite3.Connection) -> list:
    rows = db.execute(
        "SELECT * FROM patterns WHERE status IN ('active_provisional','active_verified')"
        " AND invalid_at IS NULL ORDER BY status DESC, updated_at DESC").fetchall()
    return [dict(r) for r in rows]


def cmd_export_context(_args) -> int:
    """Действующие правила — то, что скилл подмешивает в работу следующего триажа."""
    db = connect()
    create_core(db, SKILL_ID, SCHEMA_VERSION)
    out = [{"pattern_id": p["pattern_id"], "status": p["status"],
            "scope_type": p["scope_type"], "scope_value": p["scope_value"],
            "title": p["title"], "content": p["content"],
            "trigger": json.loads(p["trigger_json"] or "{}"),
            "expected": json.loads(p["expected_json"] or "{}"),
            "confirmations": p["confirmations"]}
           for p in active_patterns(db)]
    print(json.dumps(out, ensure_ascii=False, indent=2))
    return 0


def match(pattern: dict, item: dict) -> bool:
    """Сработало ли правило на записи триажа.

    item: {"jid": ..., "chat": ..., "text": ...} — сырые значения, они НЕ сохраняются,
    хешируются только для сравнения со scope_value.
    """
    stype, svalue = pattern["scope_type"], pattern["scope_value"]
    if stype == "global":
        return True
    if stype == "keyword":
        return svalue in (item.get("text") or "").lower()
    if stype in ("chat", "contact", "group"):
        return scope_value_hash(stype, item.get("jid") or "") == svalue
    return False


def apply_patterns(item: dict, triage: dict, db: sqlite3.Connection = None) -> tuple:
    """Наложить действующие правила на результат триажа.

    Возвращает (изменённый триаж, список сработавших pattern_id). Правило переопределяет
    только те поля, которые само задаёт в `expected` — остальное оставляет как есть.
    """
    own_db = db is None
    db = db or connect()
    try:
        create_core(db, SKILL_ID, SCHEMA_VERSION)
        result, fired = dict(triage), []
        for p in active_patterns(db):
            if not match(p, item):
                continue
            expected = json.loads(p["expected_json"] or "{}")
            result.update({k: v for k, v in expected.items() if v is not None})
            fired.append(p["pattern_id"])
            db.execute("UPDATE patterns SET applied_count = COALESCE(applied_count,0)+1,"
                       " last_applied_at=? WHERE pattern_id=?", (now(), p["pattern_id"]))
            db.execute("INSERT INTO pattern_application(pattern_id, result, ts)"
                       " VALUES(?,?,?)", (p["pattern_id"], "unknown", now()))
        db.commit()
        return result, fired
    finally:
        if own_db:
            db.close()


def cmd_apply(args) -> int:
    payload = json.loads(Path(args.input).read_text(encoding="utf-8-sig"))  # utf-8-sig: терпим BOM от Windows-инструментов
    triage, fired = apply_patterns(payload.get("item") or {}, payload.get("triage") or {})
    print(json.dumps({"triage": triage, "fired": fired}, ensure_ascii=False))
    return 0


def cmd_confirm(args) -> int:
    db = connect()
    create_core(db, SKILL_ID, SCHEMA_VERSION)
    row = db.execute("SELECT * FROM patterns WHERE pattern_id=?", (args.pattern_id,)).fetchone()
    if row is None:
        print(f"неизвестное правило: {args.pattern_id}", file=sys.stderr)
        return 2
    n = row["confirmations"] + 1
    status = "active_verified" if n >= VERIFIED_THRESHOLD else row["status"]
    db.execute("UPDATE patterns SET confirmations=?, status=?, updated_at=?,"
               " last_confirmed_at=? WHERE pattern_id=?",
               (n, status, now(), now(), args.pattern_id))
    log_history(db, args.pattern_id, "PROMOTE" if status != row["status"] else "UPDATE",
                "human", row["status"], status, args.reason)
    db.commit()
    print(json.dumps({"pattern_id": args.pattern_id, "confirmations": n,
                      "status": status}, ensure_ascii=False))
    return 0


def cmd_retire(args) -> int:
    """Правило не удаляется — помечается недействующим с причиной (контракт v0.2)."""
    db = connect()
    create_core(db, SKILL_ID, SCHEMA_VERSION)
    assert_writable(db, args.pattern_id, "human")
    db.execute("UPDATE patterns SET status='retired', invalid_at=?, retire_reason=?,"
               " updated_at=? WHERE pattern_id=?",
               (now(), args.reason, now(), args.pattern_id))
    log_history(db, args.pattern_id, "RETIRE", "human", None, None, args.reason)
    db.commit()
    print(json.dumps({"pattern_id": args.pattern_id, "status": "retired"}, ensure_ascii=False))
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description="store обучения скилла bip-triage")
    sub = ap.add_subparsers(dest="cmd", required=True)
    sub.add_parser("init").set_defaults(fn=cmd_init)
    p = sub.add_parser("record-feedback"); p.add_argument("--input", required=True)
    p.set_defaults(fn=cmd_record_feedback)
    sub.add_parser("export-context").set_defaults(fn=cmd_export_context)
    p = sub.add_parser("apply"); p.add_argument("--input", required=True)
    p.set_defaults(fn=cmd_apply)
    p = sub.add_parser("confirm-pattern"); p.add_argument("--pattern-id", required=True)
    p.add_argument("--reason"); p.set_defaults(fn=cmd_confirm)
    p = sub.add_parser("retire-pattern"); p.add_argument("--pattern-id", required=True)
    p.add_argument("--reason", required=True); p.set_defaults(fn=cmd_retire)
    args = ap.parse_args()
    return args.fn(args)


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    sys.exit(main())
