from __future__ import annotations

import importlib.util
import json
import sys
import tempfile
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "skills" / "route-coding-task" / "scripts" / "run_coding_task.py"
spec = importlib.util.spec_from_file_location("aircoder_resume_safety", RUNNER)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
sys.modules[spec.name] = module
spec.loader.exec_module(module)


def task_payload(repo: Path) -> dict:
    return {
        "schema_version": 1,
        "task_id": "../escape",
        "product": "air-coder",
        "repo_root": str(repo),
        "objective": "resume task-id path safety",
        "allowed_paths": ["air-coder/"],
        "context_files": ["air-coder/README.md"],
        "acceptance_commands": ["python -V"],
    }


class ResumeTaskIdSafetyAC04(unittest.TestCase):
    def test_resume_task_id_cannot_escape_run_root(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            run_root = base / "runs"
            run_root.mkdir()
            outside = base / "escape"
            outside.mkdir()
            task = task_payload(ROOT.parent)
            requested = base / "request.json"
            canonical = outside / "task.json"
            requested.write_text(json.dumps(task), encoding="utf-8")
            canonical.write_text(json.dumps(task), encoding="utf-8")
            state = {
                "schema_version": 1,
                "task_id": "../escape",
                "task_contract": str(canonical),
                "status": "preflight_passed",
            }
            (outside / "state.json").write_text(json.dumps(state), encoding="utf-8")
            with self.assertRaises(module.ContractError):
                module.load_run(requested, run_root)


if __name__ == "__main__":
    unittest.main()
