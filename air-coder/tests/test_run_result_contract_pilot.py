import importlib.util
import json
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "skills" / "route-coding-task" / "scripts" / "run_coding_task.py"
SPEC = importlib.util.spec_from_file_location("aircoder_run_coding_task_pilot", RUNNER)
module = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = module
SPEC.loader.exec_module(module)


class RunResultContractPilot(unittest.TestCase):
    def test_build_result_matches_canonical_contract(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            subprocess.run(["git", "init"], cwd=repo, check=True, capture_output=True)
            task = {
                "task_id": "AIRCODER-AC03-PILOT-001",
                "product": "air-coder",
                "repo_root": str(repo),
                "objective": "canonical result contract",
                "allowed_paths": ["air-coder/"],
                "context_files": ["README.md"],
                "acceptance_commands": ["python -V"],
                "scarce_quota_burden": "medium",
                "executor": {"model": "gpt-5.6-sol"},
            }
            state = {
                "status": "accepted",
                "thread_id": "thread-1",
                "executor_attempts": 1,
                "repair_attempts": 0,
                "executor_runs": [{"usage": {"input_tokens": 100, "output_tokens": 20}}],
                "acceptance_runs": [[
                    {"command": "git diff --check", "passed": True},
                    {"command": "python -V", "passed": True},
                ]],
                "changed_paths": ["air-coder/example.py"],
                "run_dir": str(repo / ".aircoder-run"),
            }
            result = module.build_result(task, state, time.monotonic() - 2.0)
            schema = json.loads((ROOT / "contracts" / "run-result.schema.json").read_text(encoding="utf-8"))
            Draft202012Validator(schema).validate(result)
            self.assertEqual(result["route"], "native_cli")
            self.assertEqual(result["acceptance"], {"accepted": 2, "total": 2})
            self.assertEqual(result["direct_cost_usd"], None)
            self.assertEqual(result["scarce_quota_burden"], "medium")
            self.assertEqual(result["attempts"], 1)
            self.assertTrue(result["evidence"])


if __name__ == "__main__":
    unittest.main()
