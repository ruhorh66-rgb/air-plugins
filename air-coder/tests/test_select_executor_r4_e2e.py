from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
SELECTOR = ROOT / "skills" / "route-coding-task" / "scripts" / "select_executor.py"
spec = importlib.util.spec_from_file_location("aircoder_selector_r4_e2e", SELECTOR)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
sys.modules[spec.name] = module
spec.loader.exec_module(module)


class R4OrdinaryEntryRegression(unittest.TestCase):
    def test_auto_native_preference_resolves_to_ready_codex_handoff(self) -> None:
        decision = module.select_executor({"native_preference": "auto"})
        self.assertEqual(decision["route"], "native_cli")
        self.assertEqual(decision["handoff"]["target"], "native CLI (codex)")
        self.assertEqual(decision["inputs"]["native_preference"], "auto")


if __name__ == "__main__":
    unittest.main()
