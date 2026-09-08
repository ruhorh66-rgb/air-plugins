from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "skills" / "route-coding-task" / "scripts" / "run_coding_task.py"
spec = importlib.util.spec_from_file_location("aircoder_limits_values_ac04", RUNNER)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
sys.modules[spec.name] = module
spec.loader.exec_module(module)


class LimitsValuesAC04(unittest.TestCase):
    def test_invalid_limit_values_fail_as_contract_errors(self) -> None:
        cases = [
            {"max_repair_attempts": "bad"},
            {"executor_timeout_seconds": "bad"},
            {"check_timeout_seconds": "bad"},
        ]
        for limits in cases:
            with self.subTest(limits=limits):
                with self.assertRaises(module.ContractError):
                    module.limits({"limits": limits})


if __name__ == "__main__":
    unittest.main()
