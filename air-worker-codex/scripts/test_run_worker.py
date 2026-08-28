"""Offline smoke test for the thin Codex adapter.

The adapter must load the shared worker through ``runpy`` without inheriting a
repository-local import path, starting Telegram/network work, or creating the
production runtime.  ``types`` is read-only and imports the sibling modules
that previously exposed the missing ``runtime_paths`` bootstrap.
"""
from __future__ import annotations

import os
import subprocess
import sys
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
WRAPPER = ROOT / "air-worker-codex" / "scripts" / "run_worker.py"


def run(*args: str) -> subprocess.CompletedProcess[str]:
    with tempfile.TemporaryDirectory(prefix="air-worker-codex-smoke-") as tmp:
        sandbox = Path(tmp)
        env = os.environ.copy()
        env["AIR_WORKER_RUNTIME"] = str(sandbox / "runtime")
        env["AIR_WORKER_METRICS_PATH"] = str(sandbox / "metrics.csv")
        result = subprocess.run(
            [sys.executable, str(WRAPPER), *args], cwd=sandbox, env=env,
            text=True, encoding="utf-8", capture_output=True, check=False)
        assert not (sandbox / "runtime").exists(), result
        assert not (sandbox / "metrics.csv").exists(), result
        return result


def main() -> int:
    listed = run("types")
    assert listed.returncode == 0, listed.stderr
    assert "openrouter-llm" in listed.stdout and "qwen-local" in listed.stdout, listed.stdout

    invalid = run("not-a-command")
    assert invalid.returncode == 2, invalid
    assert "команды:" in invalid.stderr, invalid.stderr
    print("run_worker adapter smoke ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
