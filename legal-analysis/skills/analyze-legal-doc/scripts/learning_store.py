#!/usr/bin/env python3
"""Local learning store for the analyze-legal-doc skill.

Implements the shared skill-learning contract v0.1: a `learning_meta` passport,
`feedback` (raw corrections) and `patterns` (derived rules) with the same field
names every skill uses, so one analyzer can read every skill's database in a
single pass.

Skill-specific extension: `verified_norms` — an accumulating base of norms already
checked against a primary source, so the same article is not re-verified from
scratch every session and fruitless searches are not repeated.

Privacy: never stores document text, extracted bodies, attachment bytes or
credentials. External objects are referenced only through an HMAC of their
locator.

Usage:
    python learning_store.py init
    python learning_store.py record-feedback --input feedback.json
    python learning_store.py record-norm --input norm.json
    python learning_store.py lookup-norm --act "ГК РФ" --article "431.2"
    python learning_store.py export-context
    python learning_store.py confirm-pattern --pattern-id LGL-PAT-000001
"""

from __future__ import annotations

import argparse
import hashlib
import hmac
import json
import os
import sqlite3
import sys
from datetime import datetime, timezone
from pathlib import Path

from store_schema import (  # vendored shared contract, see store_schema.py
    CONTRACT_VERSION,
    assert_writable,
    create_core,
    log_history,
)

SCHEMA_VERSION = "2"
SKILL_ID = "legal-analysis"
FEEDBACK_PREFIX = "LGL-FBK"
PATTERN_PREFIX = "LGL-PAT"
NORM_PREFIX = "LGL-NRM"
VERIFIED_THRESHOLD = 3

VALID_SCOPES = {"document", "author", "document_type", "project", "norm", "global"}

# Five outcomes, not two. The distinction that matters most is between a thesis
# the source CONTRADICTS and a question the source does not COVER: treating the
# second as the first manufactures accusations of hallucination.
#   confirmed     — the source supports the thesis as stated
#   defect        — the source contradicts it or it is materially imprecise
#   reinforced    — supported, and resting on a rule the author never cited
#   unresolved    — searched, nothing found (the search itself is the result)
#   unverifiable  — the source cannot answer this in principle (out of coverage,
#                   paywalled, unpublished); requires coverage_note
VALID_OUTCOMES = {"confirmed", "defect", "reinforced", "unresolved", "unverifiable"}


def state_dir() -> Path:
    override = os.environ.get("LEGAL_ANALYSIS_STATE_DIR")
    if override:
        return Path(override)
    local = os.environ.get("LOCALAPPDATA")
    if local:
        return Path(local) / "Tech77" / "LegalAnalysis"
    xdg = os.environ.get("XDG_DATA_HOME")
    if xdg:
        return Path(xdg) / "tech77-legal-analysis"
    return Path.home() / ".local" / "share" / "tech77-legal-analysis"


def now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def hmac_key(directory: Path) -> bytes:
    """Key used to derive subject_ref. Generated once, kept beside the database."""
    path = directory / "subject_ref.key"
    if path.exists():
        return path.read_bytes()
    key = os.urandom(32)
    path.write_bytes(key)
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass  # best effort; Windows ACLs are not chmod-shaped
    return key


def subject_ref(directory: Path, locator: dict) -> str:
    """Derive a stable, non-reversible reference to an external object."""
    canonical = json.dumps(locator, sort_keys=True, ensure_ascii=False).encode("utf-8")
    return hmac.new(hmac_key(directory), canonical, hashlib.sha256).hexdigest()


def connect(directory: Path) -> sqlite3.Connection:
    directory.mkdir(parents=True, exist_ok=True)
    database = sqlite3.connect(directory / "legal-analysis.sqlite")
    database.row_factory = sqlite3.Row
    return database


def create_schema(database: sqlite3.Connection) -> None:
    """Contract core (v0.2) plus this skill's own extension table."""
    create_core(database, SKILL_ID, SCHEMA_VERSION, subject_ref_column="subject_ref")
    database.executescript(
        """
        -- Skill extension: norms already checked against a primary source.
        -- `outcome` carries the three-way verification result, including
        -- 'unverifiable' — a source not covering a question is NOT evidence that
        -- the citation was invented, and collapsing the two produces false
        -- accusations of hallucination.
        CREATE TABLE IF NOT EXISTS verified_norms (
            norm_id        TEXT PRIMARY KEY,
            act            TEXT NOT NULL,
            article        TEXT NOT NULL,
            outcome        TEXT NOT NULL,
            summary        TEXT NOT NULL,
            verified_against TEXT NOT NULL,
            source_url     TEXT,
            card_ref       TEXT,
            coverage_note  TEXT,
            usage_count    INTEGER NOT NULL DEFAULT 1,
            verified_at    TEXT NOT NULL,
            updated_at     TEXT NOT NULL
        );

        CREATE UNIQUE INDEX IF NOT EXISTS idx_norm_identity
            ON verified_norms(act, article);
        """
    )
    database.commit()


def next_id(database: sqlite3.Connection, table: str, column: str, prefix: str) -> str:
    rows = database.execute(f"SELECT {column} AS identifier FROM {table}").fetchall()
    maximum = 0
    for row in rows:
        try:
            maximum = max(maximum, int(str(row["identifier"]).rsplit("-", 1)[1]))
        except (IndexError, ValueError):
            continue
    return f"{prefix}-{maximum + 1:06d}"


def cmd_init(args: argparse.Namespace) -> dict:
    directory = state_dir()
    database = connect(directory)
    create_schema(database)
    hmac_key(directory)
    return {
        "initialized": True,
        "skill_id": SKILL_ID,
        "contract_version": CONTRACT_VERSION,
        "state_dir": str(directory),
        "database": str(directory / "legal-analysis.sqlite"),
    }


def cmd_record_feedback(args: argparse.Namespace) -> dict:
    payload = json.loads(Path(args.input).read_text(encoding="utf-8"))
    for field in ("initial", "correction", "scope"):
        if field not in payload:
            raise SystemExit(f"missing required field: {field}")
    scope = payload["scope"]
    if scope.get("type") not in VALID_SCOPES:
        raise SystemExit(f"scope.type must be one of: {', '.join(sorted(VALID_SCOPES))}")

    directory = state_dir()
    database = connect(directory)
    create_schema(database)

    ref = subject_ref(directory, payload.get("subject_locator", {}))
    feedback_id = next_id(database, "feedback", "feedback_id", FEEDBACK_PREFIX)
    timestamp = now()
    pattern_id = None

    if payload.get("pattern"):
        pattern = payload["pattern"]
        pattern_id = next_id(database, "patterns", "pattern_id", PATTERN_PREFIX)
        database.execute(
            "INSERT INTO patterns(pattern_id, status, scope_type, scope_value,"
            " trigger_json, expected_json, forbidden_actions_json, confirmations,"
            " counterexamples, version, created_at, updated_at)"
            " VALUES(?,?,?,?,?,?,?,1,0,1,?,?)",
            (
                pattern_id,
                "active_provisional",
                scope["type"],
                scope["value"],
                json.dumps(pattern.get("trigger", {}), ensure_ascii=False),
                json.dumps(pattern.get("expected", {}), ensure_ascii=False),
                json.dumps(pattern.get("forbidden_actions", []), ensure_ascii=False),
                timestamp,
                timestamp,
            ),
        )

    database.execute(
        "INSERT INTO feedback(feedback_id, subject_ref, platform, initial_json,"
        " correction_json, scope_type, scope_value, pattern_id, created_at)"
        " VALUES(?,?,?,?,?,?,?,?,?)",
        (
            feedback_id,
            ref,
            payload.get("platform", "claude"),
            json.dumps(payload["initial"], ensure_ascii=False),
            json.dumps(payload["correction"], ensure_ascii=False),
            scope["type"],
            scope["value"],
            pattern_id,
            timestamp,
        ),
    )
    database.commit()
    return {
        "feedback_id": feedback_id,
        "subject_ref": ref,
        "pattern_id": pattern_id,
        "pattern_status": "active_provisional" if pattern_id else None,
        "created_at": timestamp,
    }


def cmd_record_norm(args: argparse.Namespace) -> dict:
    """Store a norm checked against a primary source, including 'unresolved'.

    Recording an unresolved search is deliberate: it stops the next session from
    repeating a fruitless lookup.
    """
    payload = json.loads(Path(args.input).read_text(encoding="utf-8"))
    for field in ("act", "article", "outcome", "summary", "verified_against"):
        if field not in payload:
            raise SystemExit(f"missing required field: {field}")
    if payload["outcome"] not in VALID_OUTCOMES:
        raise SystemExit(f"outcome must be one of: {', '.join(sorted(VALID_OUTCOMES))}")

    database = connect(state_dir())
    create_schema(database)
    timestamp = now()

    existing = database.execute(
        "SELECT norm_id, usage_count FROM verified_norms WHERE act=? AND article=?",
        (payload["act"], payload["article"]),
    ).fetchone()

    if existing:
        database.execute(
            "UPDATE verified_norms SET outcome=?, summary=?, verified_against=?,"
            " source_url=?, card_ref=?, usage_count=usage_count+1, updated_at=?"
            " WHERE norm_id=?",
            (
                payload["outcome"],
                payload["summary"],
                payload["verified_against"],
                payload.get("source_url"),
                payload.get("card_ref"),
                timestamp,
                existing["norm_id"],
            ),
        )
        database.commit()
        return {"norm_id": existing["norm_id"], "updated": True,
                "usage_count": existing["usage_count"] + 1}

    norm_id = next_id(database, "verified_norms", "norm_id", NORM_PREFIX)
    database.execute(
        "INSERT INTO verified_norms(norm_id, act, article, outcome, summary,"
        " verified_against, source_url, card_ref, usage_count, verified_at, updated_at)"
        " VALUES(?,?,?,?,?,?,?,?,1,?,?)",
        (
            norm_id,
            payload["act"],
            payload["article"],
            payload["outcome"],
            payload["summary"],
            payload["verified_against"],
            payload.get("source_url"),
            payload.get("card_ref"),
            timestamp,
            timestamp,
        ),
    )
    database.commit()
    return {"norm_id": norm_id, "created": True, "outcome": payload["outcome"]}


def cmd_lookup_norm(args: argparse.Namespace) -> dict:
    database = connect(state_dir())
    create_schema(database)
    query = "SELECT * FROM verified_norms WHERE act LIKE ?"
    params = [f"%{args.act}%"]
    if args.article:
        query += " AND article LIKE ?"
        params.append(f"%{args.article}%")
    rows = database.execute(query, params).fetchall()
    return {"found": len(rows), "norms": [dict(row) for row in rows]}


def cmd_confirm_pattern(args: argparse.Namespace) -> dict:
    """Record a confirmation or counterexample, with traceable evidence.

    A bare counter cannot be re-checked or rebuilt; every increment is therefore
    also written to pattern_evidence, tied to the feedback that produced it.
    """
    database = connect(state_dir())
    create_schema(database)
    row = database.execute(
        "SELECT * FROM patterns WHERE pattern_id=?", (args.pattern_id,)
    ).fetchone()
    if row is None:
        raise SystemExit(f"unknown pattern: {args.pattern_id}")

    actor = "human" if args.human else "skill"
    try:
        assert_writable(database, args.pattern_id, actor)
    except PermissionError as exc:
        log_history(database, args.pattern_id, "DEMOTE", "analyzer",
                    reason=f"proposed change refused: {exc}")
        database.commit()
        raise SystemExit(str(exc))

    timestamp = now()
    polarity = -1 if args.counterexample else 1

    if args.feedback_id:
        database.execute(
            "INSERT OR IGNORE INTO pattern_evidence(pattern_id, feedback_id,"
            " polarity, weight, ts) VALUES(?,?,?,1.0,?)",
            (args.pattern_id, args.feedback_id, polarity, timestamp),
        )

    if args.counterexample:
        counter = row["counterexamples"] + 1
        # Two counterexamples suspend the rule rather than silently keeping it.
        status = "suspended" if counter >= 2 else row["status"]
        database.execute(
            "UPDATE patterns SET counterexamples=?, status=?, updated_at=?"
            " WHERE pattern_id=?",
            (counter, status, timestamp, args.pattern_id),
        )
        log_history(database, args.pattern_id, "TAG", actor,
                    new_json=json.dumps({"counterexamples": counter,
                                         "status": status}, ensure_ascii=False),
                    reason=args.reason)
        database.commit()
        return {"pattern_id": args.pattern_id, "counterexamples": counter,
                "status": status, "evidence_recorded": bool(args.feedback_id)}

    confirmations = row["confirmations"] + 1
    status = "active_verified" if confirmations >= VERIFIED_THRESHOLD else row["status"]
    database.execute(
        "UPDATE patterns SET confirmations=?, status=?, last_confirmed_at=?,"
        " updated_at=? WHERE pattern_id=?",
        (confirmations, status, timestamp, timestamp, args.pattern_id),
    )
    log_history(database, args.pattern_id,
                "PROMOTE" if status != row["status"] else "TAG", actor,
                new_json=json.dumps({"confirmations": confirmations,
                                     "status": status}, ensure_ascii=False),
                reason=args.reason)
    database.commit()
    return {"pattern_id": args.pattern_id, "confirmations": confirmations,
            "status": status, "evidence_recorded": bool(args.feedback_id)}


def cmd_add_rule(args: argparse.Namespace) -> dict:
    """Add a rule stated directly by the decision-maker.

    Such a rule is active immediately — it needs no confirmation threshold, because
    it did not come from inference — and is marked origin='human'/locked so the
    machine can propose changes to it but never apply them.
    """
    payload = json.loads(Path(args.input).read_text(encoding="utf-8"))
    for field in ("title", "description", "content", "scope"):
        if field not in payload:
            raise SystemExit(f"missing required field: {field}")
    scope = payload["scope"]
    if scope.get("type") not in VALID_SCOPES:
        raise SystemExit(f"scope.type must be one of: {', '.join(sorted(VALID_SCOPES))}")

    database = connect(state_dir())
    create_schema(database)
    pattern_id = next_id(database, "patterns", "pattern_id", PATTERN_PREFIX)
    timestamp = now()

    database.execute(
        "INSERT INTO patterns(pattern_id, status, scope_type, scope_value,"
        " trigger_json, expected_json, forbidden_actions_json, confirmations,"
        " counterexamples, version, created_at, updated_at, origin, locked,"
        " rule_key, title, description, content, kind, last_confirmed_at)"
        " VALUES(?,?,?,?,?,?,?,1,0,1,?,?,'human',1,?,?,?,?,?,?)",
        (
            pattern_id,
            "active_verified",          # stated, not inferred — no threshold applies
            scope["type"],
            scope["value"],
            json.dumps(payload.get("trigger", {}), ensure_ascii=False),
            json.dumps(payload.get("expected", {}), ensure_ascii=False),
            json.dumps(payload.get("forbidden_actions", []), ensure_ascii=False),
            timestamp,
            timestamp,
            payload.get("rule_key"),
            payload["title"],
            payload["description"],
            payload["content"],
            payload.get("kind", "rule"),
            timestamp,
        ),
    )
    log_history(database, pattern_id, "ADD", "human",
                new_json=json.dumps({"title": payload["title"],
                                     "status": "active_verified",
                                     "origin": "human"}, ensure_ascii=False),
                reason=payload.get("reason", "rule stated by the decision-maker"))
    database.commit()
    return {"pattern_id": pattern_id, "status": "active_verified",
            "origin": "human", "locked": True, "title": payload["title"]}


def cmd_record_application(args: argparse.Namespace) -> dict:
    """A rule can be confirmed often and still never fire — track both."""
    database = connect(state_dir())
    create_schema(database)
    timestamp = now()
    database.execute(
        "INSERT INTO pattern_application(pattern_id, feedback_id, result, ts)"
        " VALUES(?,?,?,?)",
        (args.pattern_id, args.feedback_id, args.result, timestamp),
    )
    database.execute(
        "UPDATE patterns SET applied_count=COALESCE(applied_count,0)+1,"
        " last_applied_at=?, updated_at=? WHERE pattern_id=?",
        (timestamp, timestamp, args.pattern_id),
    )
    database.commit()
    row = database.execute(
        "SELECT applied_count FROM patterns WHERE pattern_id=?", (args.pattern_id,)
    ).fetchone()
    return {"pattern_id": args.pattern_id, "result": args.result,
            "applied_count": row["applied_count"] if row else None}


def cmd_supersede(args: argparse.Namespace) -> dict:
    """Replace a rule without deleting it: history stays reconstructible."""
    database = connect(state_dir())
    create_schema(database)
    actor = "human" if args.human else "analyzer"
    assert_writable(database, args.old_id, actor)
    timestamp = now()
    old = database.execute(
        "SELECT * FROM patterns WHERE pattern_id=?", (args.old_id,)
    ).fetchone()
    if old is None:
        raise SystemExit(f"unknown pattern: {args.old_id}")
    database.execute(
        "UPDATE patterns SET status='deprecated', invalid_at=?, superseded_by=?,"
        " retire_reason=?, updated_at=? WHERE pattern_id=?",
        (timestamp, args.new_id, args.reason, timestamp, args.old_id),
    )
    log_history(database, args.old_id, "SUPERSEDE", actor,
                old_json=json.dumps({"status": old["status"]}, ensure_ascii=False),
                new_json=json.dumps({"status": "deprecated",
                                     "superseded_by": args.new_id},
                                    ensure_ascii=False),
                reason=args.reason)
    database.commit()
    return {"deprecated": args.old_id, "superseded_by": args.new_id,
            "reason": args.reason}


def cmd_export_context(args: argparse.Namespace) -> dict:
    database = connect(state_dir())
    create_schema(database)
    patterns = database.execute(
        "SELECT * FROM patterns WHERE status IN ('active_provisional','active_verified')"
        " ORDER BY pattern_id"
    ).fetchall()
    norms = database.execute(
        "SELECT act, article, outcome, summary, verified_against, card_ref, usage_count"
        " FROM verified_norms ORDER BY act, article"
    ).fetchall()
    return {
        "schema_version": SCHEMA_VERSION,
        "skill_id": SKILL_ID,
        "active_patterns": [
            {
                "pattern_id": row["pattern_id"],
                "status": row["status"],
                "scope": {"type": row["scope_type"], "value": row["scope_value"]},
                "trigger": json.loads(row["trigger_json"]),
                "expected": json.loads(row["expected_json"]),
                "forbidden_actions": json.loads(row["forbidden_actions_json"]),
                "confirmations": row["confirmations"],
                "counterexamples": row["counterexamples"],
            }
            for row in patterns
        ],
        "verified_norms": [dict(row) for row in norms],
    }


def main() -> None:
    # Output is JSON with non-ASCII content. A console on a legacy code page
    # (cp1252/cp866) would raise UnicodeEncodeError on write, so force UTF-8
    # rather than depending on the terminal's encoding.
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except (AttributeError, OSError):
        pass

    parser = argparse.ArgumentParser(description="legal-analysis learning store")
    subparsers = parser.add_subparsers(dest="command", required=True)

    subparsers.add_parser("init").set_defaults(func=cmd_init)

    feedback = subparsers.add_parser("record-feedback")
    feedback.add_argument("--input", required=True)
    feedback.set_defaults(func=cmd_record_feedback)

    norm = subparsers.add_parser("record-norm")
    norm.add_argument("--input", required=True)
    norm.set_defaults(func=cmd_record_norm)

    lookup = subparsers.add_parser("lookup-norm")
    lookup.add_argument("--act", required=True)
    lookup.add_argument("--article")
    lookup.set_defaults(func=cmd_lookup_norm)

    confirm = subparsers.add_parser("confirm-pattern")
    confirm.add_argument("--pattern-id", required=True)
    confirm.add_argument("--counterexample", action="store_true")
    confirm.add_argument("--feedback-id", help="ties the increment to traceable evidence")
    confirm.add_argument("--reason")
    confirm.add_argument("--human", action="store_true",
                         help="acting as the decision-maker, not the machine")
    confirm.set_defaults(func=cmd_confirm_pattern)

    addrule = subparsers.add_parser("add-rule",
                                    help="rule stated by the decision-maker (locked)")
    addrule.add_argument("--input", required=True)
    addrule.set_defaults(func=cmd_add_rule)

    applied = subparsers.add_parser("record-application")
    applied.add_argument("--pattern-id", required=True)
    applied.add_argument("--result", required=True,
                         choices=["helped", "harmed", "neutral", "unknown"])
    applied.add_argument("--feedback-id")
    applied.set_defaults(func=cmd_record_application)

    supersede = subparsers.add_parser("supersede")
    supersede.add_argument("--old-id", required=True)
    supersede.add_argument("--new-id", required=True)
    supersede.add_argument("--reason", required=True)
    supersede.add_argument("--human", action="store_true")
    supersede.set_defaults(func=cmd_supersede)

    subparsers.add_parser("export-context").set_defaults(func=cmd_export_context)

    args = parser.parse_args()
    result = args.func(args)
    json.dump({"ok": True, "result": result}, sys.stdout, ensure_ascii=False, indent=2)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
