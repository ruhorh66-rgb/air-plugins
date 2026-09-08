from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "skills" / "route-coding-task" / "scripts" / "run_coding_task.py"
spec = importlib.util.spec_from_file_location("aircoder_runner_ac04_limits", RUNNER)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
sys.modules[spec.name] = module
spec.loader.exec_module(module)


class LimitsTypeAC04(unittest.TestCase):
    def test_non_object_limits_fails_closed(self) -> None:
        task = {
            "schema_version": 1,
            "task_id": "AC04-LIMITS-TYPE",
            "product": "air-coder",
            "repo_root": str(ROOT.parent),
            "objective": "validate limits object contract",
            "allowed_paths": ["air-coder/"],
            "context_files": ["air-coder/README.md"],
            "acceptance_commands": ["python -V"],
            "limits": [],
        }
        with self.assertRaises(module.ContractError):
            module.validate_task(task)


if __name__ == "__main__":
    unittest.main()
