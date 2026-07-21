"""Self-check for the norm store's coverage_note contract.

Run: python test_learning_store.py

Exists because of a real defect found on 2026-07-21 while seeding the store from
live case material: record-norm accepted `coverage_note`, the table had the
column, and the INSERT silently dropped it — so an `unverifiable` verdict lost
the one field that distinguishes "the source cannot answer this" from "nobody
looked". The store looked healthy and was quietly lossy.
"""
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).parent
STORE = HERE / "learning_store.py"


def run(state_dir, *args, payload=None):
    env = dict(os.environ, LEGAL_ANALYSIS_STATE_DIR=str(state_dir),
               PYTHONIOENCODING="utf-8")
    argv = [sys.executable, str(STORE), *args]
    if payload is not None:
        file = Path(state_dir) / "payload.json"
        file.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
        argv += ["--input", str(file)]
    return subprocess.run(argv, capture_output=True, text=True, encoding="utf-8",
                          cwd=str(HERE), env=env)


def main():
    with tempfile.TemporaryDirectory() as state_dir:
        assert run(state_dir, "init").returncode == 0, "init failed"

        note = "норма опубликована, но пинпойнт-цитата — по проприетарной пагинации"
        unverifiable = {
            "act": "Тестовый акт", "article": "ст. 1", "outcome": "unverifiable",
            "summary": "проверка невозможна по существу", "verified_against": "тест",
            "coverage_note": note,
        }
        assert run(state_dir, "record-norm", payload=unverifiable).returncode == 0

        database = sqlite3.connect(Path(state_dir) / "legal-analysis.sqlite")
        stored = database.execute(
            "SELECT coverage_note FROM verified_norms WHERE act='Тестовый акт'"
        ).fetchone()[0]
        assert stored == note, f"coverage_note not persisted, got {stored!r}"

        # Re-recording the same norm must not wipe the note either — the UPDATE
        # branch dropped it for the same reason the INSERT did.
        updated = dict(unverifiable, summary="уточнённая формулировка")
        assert run(state_dir, "record-norm", payload=updated).returncode == 0
        stored = database.execute(
            "SELECT coverage_note FROM verified_norms WHERE act='Тестовый акт'"
        ).fetchone()[0]
        assert stored == note, f"coverage_note lost on update, got {stored!r}"

        # And the documented requirement is enforced, not merely described.
        missing = {k: v for k, v in unverifiable.items() if k != "coverage_note"}
        missing["article"] = "ст. 2"
        result = run(state_dir, "record-norm", payload=missing)
        assert result.returncode != 0, "unverifiable without coverage_note was accepted"
        assert "coverage_note" in (result.stderr + result.stdout)

        context = json.loads(run(state_dir, "export-context").stdout)
        assert "coverage_note" in context["result"]["verified_norms"][0], \
            "export-context hides coverage_note from the next session"

        # Windows will not delete the temp directory while the handle is open.
        database.close()

    print("ok: coverage_note persists, survives update, is required, and is exported")


if __name__ == "__main__":
    main()
