"""Thin Codex entrypoint for the shared air-worker Python core.

No executor, contract, runtime, or secret is copied here: keeping this adapter
small prevents Claude and Codex behavior from drifting.
"""
from __future__ import annotations

import runpy
import sys
from pathlib import Path


SHARED_WORKER = (Path(__file__).resolve().parents[2] / "air-worker" / "skills" /
                 "run-worker-task" / "scripts" / "worker.py")

if not SHARED_WORKER.is_file():
    raise SystemExit("shared air-worker core is unavailable")

# ``runpy.run_path`` sets ``__file__`` but does not place the target directory
# on ``sys.path``.  The shared core imports sibling modules such as
# ``runtime_paths``, so make its scripts directory the first import location
# without copying any core code into this adapter.
SHARED_SCRIPTS = str(SHARED_WORKER.parent)
if SHARED_SCRIPTS in sys.path:
    sys.path.remove(SHARED_SCRIPTS)
sys.path.insert(0, SHARED_SCRIPTS)

runpy.run_path(str(SHARED_WORKER), run_name="__main__")
