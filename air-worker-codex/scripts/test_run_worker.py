"""Offline smoke test for the thin Codex adapter.

The adapter must load the shared worker through ``runpy`` without inheriting a
repository-local import path, starting Telegram/network work, or creating the
production runtime.  ``types`` is read-only and imports the sibling modules
that previously exposed the missing ``runtime_paths`` bootstrap.
"""
from __future__ import annotations

import os
import json
import subprocess
import sys
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CANONICAL_PLUGIN = ROOT / "air-worker"
ADAPTER_PLUGIN = ROOT / "air-worker-codex"
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


def test_packaging_contract() -> None:
    """The canonical package owns the core; the adapter only points at it."""
    canonical = json.loads((CANONICAL_PLUGIN / ".codex-plugin" / "plugin.json").read_text(
        encoding="utf-8"))
    adapter = json.loads((ADAPTER_PLUGIN / ".codex-plugin" / "plugin.json").read_text(
        encoding="utf-8"))
    assert canonical["name"] == CANONICAL_PLUGIN.name == "air-worker", canonical
    assert adapter["name"] == ADAPTER_PLUGIN.name == "air-worker-codex", adapter
    assert canonical["version"] == adapter["version"] == "0.3.0"
    assert canonical["skills"] == adapter["skills"] == "./skills/"
    assert isinstance(canonical.get("interface"), dict) and canonical["interface"].get("displayName")
    assert isinstance(adapter.get("interface"), dict) and adapter["interface"].get("displayName")

    shared_scripts = CANONICAL_PLUGIN / "skills" / "run-worker-task" / "scripts"
    for filename in ("worker.py", "executors.py", "runtime_paths.py"):
        assert (shared_scripts / filename).is_file(), filename
        assert not (ADAPTER_PLUGIN / "scripts" / filename).exists(), filename


def main() -> int:
    test_packaging_contract()
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
