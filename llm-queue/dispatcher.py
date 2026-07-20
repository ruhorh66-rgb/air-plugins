#!/usr/bin/env python3
r"""Job dispatcher for the local llama-server contour.

Fills the gap llm_client.py explicitly does not cover. That module says of itself:
"Not a job broker / not a persistent service -- a file-based semaphore matching the
server's -np flag". It stops concurrent callers from oversubscribing slots; it does
not remember work, survive a restart, retry a failure or report progress.

This adds those four things and nothing else. Slot acquisition still goes through
llm_client.call_llm — the semaphore stays the single point of coordination, because
two independent slot mechanisms would defeat the purpose of having one.

    dispatcher.py enqueue --kind gtd_reframe --input card.txt --prompt-file p.txt
    dispatcher.py run --limit 20        # process the queue, oldest high-priority first
    dispatcher.py status                # what is queued, running, done, failed
    dispatcher.py retry --failed        # requeue failures that have attempts left
    dispatcher.py show --job 17         # one job with its error and result path

Design notes:
  * SQLite, not files: a queue needs atomic claim, and a directory of .json cannot
    give it without lockfile gymnastics.
  * A job claimed by a dead process is reclaimed on the next run (same reasoning as
    the stale-lock reclaim in llm_client), so a crash mid-job is not a permanent leak.
  * Results are written to disk, not into the database: model output can be large and
    a queue table full of megabytes is a queue table nobody can read.

INTERPRETER: run with the Python that has `requests` installed — the one your llm_client
already uses. A system Python without it fails only at the first job, after the queue is
already populated, so the failure looks like a queue problem and is not. Point
LLM_QUEUE_PYTHON at it if you script this.

LAYOUT: code here, state elsewhere. The queue database and results go to
LLM_QUEUE_DATA_DIR when set, otherwise beside this file. Keeping state out of the code
directory is what lets the code live in a public repository while the work it processed
stays private.
"""

from __future__ import annotations

import argparse
import json
import os
import sqlite3
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

QUEUE_DIR = Path(__file__).resolve().parent
# State lives apart from code: the queue and its results may contain the content of
# whatever was processed, which is precisely what must not follow the code into a
# public repository.
DATA_DIR = Path(os.environ.get("LLM_QUEUE_DATA_DIR", QUEUE_DIR))
DB_PATH = DATA_DIR / "jobs.sqlite"
RESULTS_DIR = DATA_DIR / "results"
MAX_ATTEMPTS = 3
CLAIM_STALE_SECONDS = 60 * 60  # a running job older than this with a dead PID is freed

# llm_client.py is the slot semaphore this dispatcher runs on top of. It may live
# beside the data rather than beside the code — the runtime contour keeps it with the
# model tooling. Search the data dir first, then here.
for _candidate in (DATA_DIR, QUEUE_DIR):
    if (_candidate / "llm_client.py").exists():
        sys.path.insert(0, str(_candidate))
        break
else:
    sys.path.insert(0, str(QUEUE_DIR))


def now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def _pid_alive(pid: int) -> bool:
    if not pid:
        return False
    if os.name != "nt":
        try:
            os.kill(pid, 0)
            return True
        except OSError:
            return False
    import subprocess
    out = subprocess.run(["tasklist", "/FI", f"PID eq {pid}"],
                         capture_output=True, text=True).stdout
    return str(pid) in out


def connect() -> sqlite3.Connection:
    db = sqlite3.connect(DB_PATH, timeout=30)
    db.row_factory = sqlite3.Row
    db.execute("PRAGMA journal_mode=WAL")
    db.executescript(
        """
        CREATE TABLE IF NOT EXISTS jobs (
            id           INTEGER PRIMARY KEY,
            kind         TEXT NOT NULL,
            input_path   TEXT,
            prompt_path  TEXT,
            prompt_inline TEXT,
            params_json  TEXT NOT NULL DEFAULT '{}',
            priority     INTEGER NOT NULL DEFAULT 5,
            status       TEXT NOT NULL DEFAULT 'queued',
            attempts     INTEGER NOT NULL DEFAULT 0,
            max_attempts INTEGER NOT NULL DEFAULT 3,
            claimed_pid  INTEGER,
            claimed_at   TEXT,
            result_path  TEXT,
            error        TEXT,
            created_at   TEXT NOT NULL,
            started_at   TEXT,
            finished_at  TEXT
        );
        CREATE INDEX IF NOT EXISTS idx_jobs_ready
            ON jobs(status, priority, id);
        """
    )
    db.commit()
    return db


def reclaim_stale(db: sqlite3.Connection) -> int:
    """Free jobs whose worker died. Without this a crash strands work forever."""
    freed = 0
    for row in db.execute("SELECT id, claimed_pid, claimed_at FROM jobs"
                          " WHERE status='running'").fetchall():
        try:
            age = time.time() - datetime.fromisoformat(row["claimed_at"]).timestamp()
        except (TypeError, ValueError):
            age = CLAIM_STALE_SECONDS + 1
        alive = _pid_alive(row["claimed_pid"] or 0)
        # A dead worker means the job is orphaned NOW — waiting out the stale window
        # would strand it for an hour after a crash that took milliseconds. The age
        # check only guards the case where the pid is alive but wedged.
        if not alive or age > CLAIM_STALE_SECONDS:
            db.execute(
                "UPDATE jobs SET status='queued', claimed_pid=NULL, claimed_at=NULL,"
                " error=COALESCE(error,'') || ' | reclaimed: worker died'"
                " WHERE id=?", (row["id"],))
            freed += 1
    if freed:
        db.commit()
    return freed


def claim_next(db: sqlite3.Connection) -> sqlite3.Row | None:
    """Atomically take one job. Highest priority first, then oldest."""
    db.execute("BEGIN IMMEDIATE")
    row = db.execute(
        "SELECT * FROM jobs WHERE status='queued' AND attempts < max_attempts"
        " ORDER BY priority ASC, id ASC LIMIT 1"
    ).fetchone()
    if row is None:
        db.execute("COMMIT")
        return None
    db.execute(
        "UPDATE jobs SET status='running', claimed_pid=?, claimed_at=?,"
        " started_at=COALESCE(started_at,?), attempts=attempts+1 WHERE id=?",
        (os.getpid(), now(), now(), row["id"]),
    )
    db.execute("COMMIT")
    return row


def build_prompt(job: sqlite3.Row) -> str:
    parts: list[str] = []
    if job["prompt_path"]:
        parts.append(Path(job["prompt_path"]).read_text(encoding="utf-8"))
    elif job["prompt_inline"]:
        parts.append(job["prompt_inline"])
    if job["input_path"]:
        text = Path(job["input_path"]).read_text(encoding="utf-8", errors="replace")
        params = json.loads(job["params_json"] or "{}")
        limit = int(params.get("input_char_limit", 12000))
        if len(text) > limit:
            # Truncate loudly: a silently cut input produces a confident answer
            # about material the model never saw.
            text = text[:limit] + f"\n\n[ОБРЕЗАНО на {limit} знаках из {len(text)}]"
        parts.append("\n\n--- ВХОДНОЙ ТЕКСТ ---\n" + text)
    return "\n\n".join(parts)


def run_one(db: sqlite3.Connection, job: sqlite3.Row, verbose: bool) -> bool:
    params = json.loads(job["params_json"] or "{}")
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    out_path = RESULTS_DIR / f"job_{job['id']:05d}_{job['kind']}.txt"
    try:
        # Imported inside the try, and late: a missing dependency (wrong interpreter,
        # no `requests`) must land in the job row as a normal failure. Outside the try
        # it kills the worker and leaves the job stuck in 'running'.
        from llm_client import call_llm
        prompt = build_prompt(job)
        if verbose:
            print(f"  [{job['id']}] {job['kind']}: промпт {len(prompt)} знаков")
        text = call_llm(
            prompt,
            client_name=f"dispatcher_{job['kind']}",
            max_tokens=int(params.get("max_tokens", 1200)),
            temperature=float(params.get("temperature", 0.1)),
            timeout=float(params.get("timeout", 300)),
            slot_timeout=float(params.get("slot_timeout", 900)),
        )
        out_path.write_text(text, encoding="utf-8")
        db.execute(
            "UPDATE jobs SET status='done', finished_at=?, result_path=?, error=NULL,"
            " claimed_pid=NULL WHERE id=?", (now(), str(out_path), job["id"]))
        db.commit()
        return True
    except Exception as exc:  # noqa: BLE001 — any failure must land in the row
        attempts = job["attempts"] + 1
        exhausted = attempts >= job["max_attempts"]
        db.execute(
            "UPDATE jobs SET status=?, finished_at=?, error=?, claimed_pid=NULL"
            " WHERE id=?",
            ("failed" if exhausted else "queued", now(),
             f"{type(exc).__name__}: {exc}"[:1000], job["id"]))
        db.commit()
        return False


# ------------------------------------------------------------------ commands

def cmd_enqueue(args: argparse.Namespace) -> None:
    db = connect()
    params: dict = {}
    if args.params:
        params = json.loads(args.params)
    cur = db.execute(
        "INSERT INTO jobs(kind, input_path, prompt_path, prompt_inline, params_json,"
        " priority, max_attempts, created_at) VALUES(?,?,?,?,?,?,?,?)",
        (args.kind, args.input, args.prompt_file, args.prompt,
         json.dumps(params, ensure_ascii=False), args.priority,
         args.max_attempts, now()))
    db.commit()
    print(f"задание {cur.lastrowid} поставлено: {args.kind} (приоритет {args.priority})")


def cmd_enqueue_batch(args: argparse.Namespace) -> None:
    """Queue one job per file — the common case for sweeping a vault."""
    db = connect()
    files = sorted(Path(args.dir).glob(args.glob))
    if not files:
        print(f"по маске {args.glob} в {args.dir} ничего не найдено")
        return
    params = json.loads(args.params) if args.params else {}
    n = skipped = 0
    for f in files:
        if not args.allow_duplicates:
            # Same input already queued, running or done for this kind → skip.
            # Without this a batch silently re-runs work already paid for, and the
            # duplicate looks like a legitimate second opinion in the results dir.
            dup = db.execute(
                "SELECT id FROM jobs WHERE kind=? AND input_path=?"
                " AND status IN ('queued','running','done') LIMIT 1",
                (args.kind, str(f))).fetchone()
            if dup:
                skipped += 1
                continue
        db.execute(
            "INSERT INTO jobs(kind, input_path, prompt_path, params_json, priority,"
            " max_attempts, created_at) VALUES(?,?,?,?,?,?,?)",
            (args.kind, str(f), args.prompt_file,
             json.dumps(params, ensure_ascii=False), args.priority,
             args.max_attempts, now()))
        n += 1
    db.commit()
    msg = f"поставлено заданий: {n} (тип {args.kind}, приоритет {args.priority})"
    if skipped:
        msg += f"; пропущено дублей: {skipped}"
    print(msg)


def cmd_run(args: argparse.Namespace) -> None:
    db = connect()
    freed = reclaim_stale(db)
    if freed:
        print(f"освобождено зависших заданий: {freed}")
    done = failed = 0
    while args.limit is None or (done + failed) < args.limit:
        job = claim_next(db)
        if job is None:
            break
        ok = run_one(db, job, args.verbose)
        if ok:
            done += 1
            print(f"  [{job['id']}] {job['kind']} — готово")
        else:
            failed += 1
            row = db.execute("SELECT status, error FROM jobs WHERE id=?",
                             (job["id"],)).fetchone()
            print(f"  [{job['id']}] {job['kind']} — {row['status']}: "
                  f"{(row['error'] or '')[:120]}")
    print(f"\nобработано: {done} успешно, {failed} с ошибкой")
    if failed:
        print("повторить: dispatcher.py retry --failed")


def cmd_status(args: argparse.Namespace) -> None:
    db = connect()
    reclaim_stale(db)
    rows = db.execute(
        "SELECT status, COUNT(*) AS n FROM jobs GROUP BY status").fetchall()
    if not rows:
        print("очередь пуста")
        return
    print("=" * 52)
    print("ОЧЕРЕДЬ ЗАДАНИЙ")
    print("=" * 52)
    for r in rows:
        print(f"  {r['status']:<10} {r['n']}")
    by_kind = db.execute(
        "SELECT kind, status, COUNT(*) AS n FROM jobs GROUP BY kind, status"
        " ORDER BY kind").fetchall()
    print("\nпо типам:")
    for r in by_kind:
        print(f"  {r['kind']:<20} {r['status']:<10} {r['n']}")
    failed = db.execute(
        "SELECT id, kind, attempts, max_attempts, error FROM jobs"
        " WHERE status='failed' ORDER BY id LIMIT 5").fetchall()
    if failed:
        print("\nошибки (первые 5):")
        for r in failed:
            print(f"  [{r['id']}] {r['kind']} попыток {r['attempts']}/"
                  f"{r['max_attempts']}: {(r['error'] or '')[:100]}")


def cmd_retry(args: argparse.Namespace) -> None:
    db = connect()
    if args.job:
        db.execute("UPDATE jobs SET status='queued', attempts=0, error=NULL"
                   " WHERE id=?", (args.job,))
        print(f"задание {args.job} возвращено в очередь")
    elif args.failed:
        cur = db.execute(
            "UPDATE jobs SET status='queued', attempts=0, error=NULL"
            " WHERE status='failed'")
        print(f"возвращено в очередь: {cur.rowcount}")
    else:
        print("укажите --job N или --failed")
        return
    db.commit()


def cmd_show(args: argparse.Namespace) -> None:
    db = connect()
    row = db.execute("SELECT * FROM jobs WHERE id=?", (args.job,)).fetchone()
    if row is None:
        print(f"задания {args.job} нет")
        return
    for key in row.keys():
        value = row[key]
        if key == "prompt_inline" and value and len(str(value)) > 200:
            value = str(value)[:200] + "…"
        print(f"  {key:<14} {value}")


def main() -> None:
    for stream in ("stdout", "stderr"):
        try:
            getattr(sys, stream).reconfigure(encoding="utf-8")
        except (AttributeError, OSError):
            pass

    ap = argparse.ArgumentParser(description="локальная очередь заданий для llama-server")
    sub = ap.add_subparsers(dest="cmd", required=True)

    e = sub.add_parser("enqueue", help="поставить одно задание")
    e.add_argument("--kind", required=True)
    e.add_argument("--input")
    e.add_argument("--prompt-file")
    e.add_argument("--prompt")
    e.add_argument("--params", help="JSON: max_tokens, temperature, input_char_limit…")
    e.add_argument("--priority", type=int, default=5)
    e.add_argument("--max-attempts", type=int, default=MAX_ATTEMPTS)
    e.set_defaults(func=cmd_enqueue)

    b = sub.add_parser("enqueue-batch", help="по заданию на файл")
    b.add_argument("--kind", required=True)
    b.add_argument("--dir", required=True)
    b.add_argument("--glob", default="*.md")
    b.add_argument("--prompt-file", required=True)
    b.add_argument("--params")
    b.add_argument("--priority", type=int, default=5)
    b.add_argument("--max-attempts", type=int, default=MAX_ATTEMPTS)
    b.add_argument("--allow-duplicates", action="store_true",
                   help="ставить задание даже если этот файл уже обработан")
    b.set_defaults(func=cmd_enqueue_batch)

    r = sub.add_parser("run", help="обработать очередь")
    r.add_argument("--limit", type=int)
    r.add_argument("--verbose", action="store_true")
    r.set_defaults(func=cmd_run)

    s = sub.add_parser("status", help="состояние очереди")
    s.set_defaults(func=cmd_status)

    t = sub.add_parser("retry", help="вернуть в очередь")
    t.add_argument("--job", type=int)
    t.add_argument("--failed", action="store_true")
    t.set_defaults(func=cmd_retry)

    sh = sub.add_parser("show", help="одно задание подробно")
    sh.add_argument("--job", type=int, required=True)
    sh.set_defaults(func=cmd_show)

    args = ap.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
