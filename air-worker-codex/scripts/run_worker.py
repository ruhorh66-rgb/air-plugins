"""Thin Codex entrypoint for the shared air-worker Python core.

No executor, contract, runtime, or secret is copied here: keeping this adapter
small prevents Claude and Codex behavior from drifting.
"""
from __future__ import annotations

import runpy
from pathlib import Path


SHARED_WORKER = (Path(__file__).resolve().parents[2] / "air-worker" / "skills" /
                 "run-worker-task" / "scripts" / "worker.py")

if not SHARED_WORKER.is_file():
    raise SystemExit("shared air-worker core is unavailable")

runpy.run_path(str(SHARED_WORKER), run_name="__main__")
