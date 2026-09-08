from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "skills" / "route-coding-task" / "scripts" / "run_coding_task.py"
spec = importlib.util.spec_from_file_location("aircoder_runner_schema_alignment", RUNNER)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
sys.modules[spec.name] = module
spec.loader.exec_module(module)


def valid_task() -> dict:
    return {
        "schema_version": 1,
        "task_id": "SCHEMA-ALIGN",
        "product": "air-coder",
        "repo_root": str(ROOT.parent),
        "objective": "validate task schema alignment",
        "allowed_paths": ["air-coder/"],
        "context_files": ["air-coder/README.md"],
        "acceptance_commands": ["python -V"],
    }


class TaskSchemaAlignmentTests(unittest.TestCase):
    def test_optional_types_fail_closed(self) -> None:
        cases = {
            "expected_remote": 123,
            "expected_head": [],
            "require_clean_start": "false",
            "executor": [],
            "limits": [],
        }
        for key, value in cases.items():
            with self.subTest(key=key):
                task = valid_task()
                task[key] = value
                with self.assertRaises(module.ContractError):
                    module.validate_task(task)

    def test_unknown_top_level_field_fails_closed(self) -> None:
        task = valid_task()
        task["surprise"] = True
        with self.assertRaises(module.ContractError):
            module.validate_task(task)

    def test_schema_aligned_optional_values_pass(self) -> None:
        task = valid_task()
        task.update({
            "expected_remote": None,
            "expected_head": "abc",
            "require_clean_start": False,
            "executor": {},
            "limits": {},
        })
        module.validate_task(task)

class RuntimeSchemaKeyTests(unittest.TestCase):
    def test_runtime_keys_match_schema_properties(self) -> None:
        import json

        schema = json.loads((ROOT / "contracts" / "coding-task.schema.json").read_text(encoding="utf-8"))
        self.assertEqual(module.TASK_KEYS, set(schema["properties"]))

    def test_task_id_constraints_match_schema_and_runtime(self) -> None:
        import json
        import re

        schema = json.loads((ROOT / "contracts" / "coding-task.schema.json").read_text(encoding="utf-8"))
        pattern = re.compile(schema["properties"]["task_id"]["pattern"])
        valid = ["TASK-1", "a.b_c-9", "A", "0"]
        invalid = ["", ".", "..", "../escape", "a/b", " a", "a ", "a\\b"]
        for value in valid:
            with self.subTest(valid=value):
                self.assertIsNotNone(pattern.fullmatch(value))
                task = valid_task()
                task["task_id"] = value
                module.validate_task(task)
        for value in invalid:
            with self.subTest(invalid=value):
                self.assertIsNone(pattern.fullmatch(value))
                task = valid_task()
                task["task_id"] = value
                with self.assertRaises(module.ContractError):
                    module.validate_task(task)

if __name__ == "__main__":
    unittest.main()
