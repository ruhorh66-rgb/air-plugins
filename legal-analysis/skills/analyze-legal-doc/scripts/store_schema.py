"""Shared skill-learning schema, contract v0.2.

Vendored into each skill rather than imported as a package: skills live in
different repositories (some public, some private) and must not depend on each
other. The file is small and stable; copy it, do not fork its semantics.

v0.2 adds, over v0.1:
  * origin/locked        — machine may not overwrite human-authored rules
  * pattern_evidence     — which episodes confirmed a rule, not just how many
  * pattern_history      — append-only log of rule edits
  * soft invalidation    — invalid_at / superseded_by / retire_reason, never DELETE
  * application tracking — how often a rule fired and how that turned out
  * privacy              — codes and hashes instead of full payloads
  * v_pattern_health     — the fixed interface a cross-skill analyzer reads

Migration is additive: ALTER TABLE ADD COLUMN and CREATE TABLE IF NOT EXISTS only.
Existing rows are never rewritten and no column is ever dropped or renamed.
"""

from __future__ import annotations

import sqlite3
from datetime import datetime, timezone

CONTRACT_VERSION = "0.2"

VALID_STATUS = ("candidate", "active_provisional", "active_verified",
                "suspended", "deprecated", "retired", "rejected")
VALID_ORIGIN = ("derived", "human", "imported")
VALID_POLARITY = (-1, 0, 1)
VALID_RESULT = ("helped", "harmed", "neutral", "unknown")
VALID_EVENT = ("ADD", "UPDATE", "TAG", "PROMOTE", "DEMOTE",
               "SUSPEND", "SUPERSEDE", "RETIRE", "REJECT")
VALID_ACTOR = ("human", "analyzer", "skill", "import")


def now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def _columns(db: sqlite3.Connection, table: str) -> set[str]:
    try:
        return {row[1] for row in db.execute(f"PRAGMA table_info({table})")}
    except sqlite3.Error:
        return set()


def _add_column(db: sqlite3.Connection, table: str, column: str, ddl: str) -> bool:
    """Additive migration. Returns True if the column was added."""
    if not _columns(db, table):
        return False  # table does not exist yet; create_core will make it
    if column in _columns(db, table):
        return False
    db.execute(f"ALTER TABLE {table} ADD COLUMN {column} {ddl}")
    return True


def create_core(db: sqlite3.Connection, skill_id: str, schema_version: str,
                subject_ref_column: str = "subject_ref") -> None:
    """Create the contract core. Safe to call on an existing v0.1 database."""
    db.executescript(
        f"""
        CREATE TABLE IF NOT EXISTS learning_meta (
            key   TEXT PRIMARY KEY,
            value TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS feedback (
            feedback_id     TEXT PRIMARY KEY,
            {subject_ref_column} TEXT NOT NULL,
            platform        TEXT NOT NULL,
            initial_json    TEXT,
            correction_json TEXT,
            scope_type      TEXT NOT NULL,
            scope_value     TEXT NOT NULL,
            pattern_id      TEXT,
            created_at      TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS patterns (
            pattern_id             TEXT PRIMARY KEY,
            status                 TEXT NOT NULL,
            scope_type             TEXT NOT NULL,
            scope_value            TEXT NOT NULL,
            trigger_json           TEXT NOT NULL,
            expected_json          TEXT NOT NULL,
            forbidden_actions_json TEXT NOT NULL,
            confirmations          INTEGER NOT NULL DEFAULT 1,
            counterexamples        INTEGER NOT NULL DEFAULT 0,
            version                INTEGER NOT NULL DEFAULT 1,
            created_at             TEXT NOT NULL,
            updated_at             TEXT NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_patterns_status
            ON patterns(status, scope_type, scope_value);

        -- v0.2: which episodes actually confirmed or contradicted a rule.
        -- Without this a counter cannot be re-checked or rebuilt.
        CREATE TABLE IF NOT EXISTS pattern_evidence (
            id          INTEGER PRIMARY KEY,
            pattern_id  TEXT NOT NULL REFERENCES patterns(pattern_id) ON DELETE CASCADE,
            feedback_id TEXT NOT NULL REFERENCES feedback(feedback_id) ON DELETE CASCADE,
            polarity    INTEGER NOT NULL CHECK (polarity IN (-1,0,1)),
            weight      REAL NOT NULL DEFAULT 1.0,
            ts          TEXT NOT NULL,
            UNIQUE (pattern_id, feedback_id)
        );

        CREATE INDEX IF NOT EXISTS idx_evidence_pattern
            ON pattern_evidence(pattern_id, polarity, ts);

        -- v0.2: append-only edit log. A rule edit never silently overwrites.
        CREATE TABLE IF NOT EXISTS pattern_history (
            id         INTEGER PRIMARY KEY,
            pattern_id TEXT NOT NULL REFERENCES patterns(pattern_id) ON DELETE CASCADE,
            rev        INTEGER NOT NULL,
            event      TEXT NOT NULL,
            old_json   TEXT,
            new_json   TEXT,
            actor      TEXT NOT NULL,
            reason     TEXT,
            ts         TEXT NOT NULL,
            UNIQUE (pattern_id, rev)
        );

        -- v0.2: a rule can be confirmed often and still never fire.
        CREATE TABLE IF NOT EXISTS pattern_application (
            id          INTEGER PRIMARY KEY,
            pattern_id  TEXT NOT NULL REFERENCES patterns(pattern_id) ON DELETE CASCADE,
            feedback_id TEXT,
            result      TEXT NOT NULL,
            ts          TEXT NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_application_pattern
            ON pattern_application(pattern_id, result);
        """
    )

    # --- ReasoningBank memory-item format (arXiv 2509.25140, ICLR 2026) ---
    # A rule is retrievable and promptable only if it has a short name, a
    # one-sentence description and 1-3 sentences of distilled steps. Storing raw
    # trigger/expected JSON alone makes rules unreadable in bulk.
    _add_column(db, "patterns", "title", "TEXT")
    _add_column(db, "patterns", "description", "TEXT")
    _add_column(db, "patterns", "content", "TEXT")
    # Rules distilled from failures, not from corrections: a guard-rail learned
    # from something going wrong. ReasoningBank reports these as the larger share
    # of the gain, and they are exactly what a corrections-only store never sees.
    _add_column(db, "patterns", "kind", "TEXT NOT NULL DEFAULT 'rule'")

    # --- additive migration of the patterns table (v0.1 -> v0.2) ---
    _add_column(db, "patterns", "origin", "TEXT NOT NULL DEFAULT 'derived'")
    _add_column(db, "patterns", "locked", "INTEGER NOT NULL DEFAULT 0")
    _add_column(db, "patterns", "rule_key", "TEXT")
    _add_column(db, "patterns", "applied_count", "INTEGER NOT NULL DEFAULT 0")
    _add_column(db, "patterns", "last_applied_at", "TEXT")
    _add_column(db, "patterns", "last_confirmed_at", "TEXT")
    _add_column(db, "patterns", "invalid_at", "TEXT")
    _add_column(db, "patterns", "superseded_by", "TEXT")
    _add_column(db, "patterns", "retire_reason", "TEXT")

    # --- privacy: codes instead of full payloads (v0.1 columns kept, unused) ---
    _add_column(db, "feedback", "initial_codes", "TEXT")
    _add_column(db, "feedback", "correction_codes", "TEXT")
    _add_column(db, "feedback", "initial_hash", "TEXT")
    _add_column(db, "feedback", "correction_hash", "TEXT")

    # Guard against duplicate rules once rule_key is in use.
    db.execute(
        "CREATE UNIQUE INDEX IF NOT EXISTS idx_pattern_rule_key "
        "ON patterns(scope_type, scope_value, rule_key) WHERE rule_key IS NOT NULL"
    )

    # --- the fixed interface a cross-skill analyzer reads ---
    db.executescript(
        f"""
        DROP VIEW IF EXISTS v_pattern_health;
        CREATE VIEW v_pattern_health AS
        SELECT
            '{skill_id}'      AS skill_id,
            p.pattern_id,
            p.status,
            COALESCE(p.origin,'derived')  AS origin,
            COALESCE(p.locked,0)          AS locked,
            p.scope_type,
            p.scope_value,
            p.confirmations,
            p.counterexamples,
            COALESCE(p.applied_count,0)   AS applied_count,
            (SELECT COUNT(*) FROM pattern_evidence e
              WHERE e.pattern_id = p.pattern_id AND e.polarity = 1)  AS evidence_pos,
            (SELECT COUNT(*) FROM pattern_evidence e
              WHERE e.pattern_id = p.pattern_id AND e.polarity = -1) AS evidence_neg,
            p.created_at,
            p.updated_at,
            COALESCE(p.last_confirmed_at, p.created_at) AS last_confirmed_at,
            p.invalid_at,
            p.superseded_by,
            CAST(julianday('now')
                 - julianday(COALESCE(p.last_confirmed_at, p.created_at)) AS INTEGER)
                AS days_stale
        FROM patterns p;
        """
    )

    for key, value in (
        ("skill_id", skill_id),
        ("contract_version", CONTRACT_VERSION),
        ("schema_version", schema_version),
        ("subject_ref_column", subject_ref_column),
    ):
        db.execute(
            "INSERT INTO learning_meta(key, value) VALUES(?, ?) "
            "ON CONFLICT(key) DO UPDATE SET value=excluded.value",
            (key, value),
        )
    db.execute(
        "INSERT OR IGNORE INTO learning_meta(key, value) VALUES('created_at', ?)",
        (now(),),
    )
    db.commit()


def log_history(db: sqlite3.Connection, pattern_id: str, event: str, actor: str,
                old_json: str | None = None, new_json: str | None = None,
                reason: str | None = None) -> int:
    """Append a revision to the rule's edit log. Returns the revision number."""
    row = db.execute(
        "SELECT COALESCE(MAX(rev), 0) AS rev FROM pattern_history WHERE pattern_id=?",
        (pattern_id,),
    ).fetchone()
    rev = (row["rev"] if hasattr(row, "keys") else row[0]) + 1
    db.execute(
        "INSERT INTO pattern_history(pattern_id, rev, event, old_json, new_json,"
        " actor, reason, ts) VALUES(?,?,?,?,?,?,?,?)",
        (pattern_id, rev, event, old_json, new_json, actor, reason, now()),
    )
    return rev


def assert_writable(db: sqlite3.Connection, pattern_id: str, actor: str) -> None:
    """Human-authored or locked rules are not machine-editable.

    The analyzer may propose a change by logging DEMOTE, but must not apply it.
    """
    row = db.execute(
        "SELECT COALESCE(origin,'derived') AS origin, COALESCE(locked,0) AS locked"
        " FROM patterns WHERE pattern_id=?",
        (pattern_id,),
    ).fetchone()
    if row is None:
        raise ValueError(f"unknown pattern: {pattern_id}")
    origin = row["origin"] if hasattr(row, "keys") else row[0]
    locked = row["locked"] if hasattr(row, "keys") else row[1]
    if actor != "human" and (locked or origin == "human"):
        raise PermissionError(
            f"{pattern_id} is human-authored or locked; actor '{actor}' may propose "
            f"a change (log DEMOTE) but not apply it"
        )
