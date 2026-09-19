from __future__ import annotations

import importlib.util
import sys
import tempfile
import unittest
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def load_hooks():
    name = f"air_worker_hooks_enforcement_{uuid.uuid4().hex}"
    spec = importlib.util.spec_from_file_location(name, ROOT / "hooks.py")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


class HookEnforcementTests(unittest.TestCase):
    def setUp(self):
        self.hooks = load_hooks()
        self.hooks.configure("airworker-hermes-v1-enforce")
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / "run-config.json").write_text("{}", encoding="utf-8")
        self.changed = self.root / "changed.py"

    def tearDown(self):
        self.temp.cleanup()

    def bind(self, session="session-a"):
        self.hooks.bind_session(session_id=session, cwd=str(self.root))

    def result(self, action="verify", exit_code=0, outcome="completed", receipts=None, log_path="verify.json"):
        return {
            "schema_version": "air-worker.tool/v1",
            "action": action,
            "exit_code": exit_code,
            "outcome": outcome,
            "progress": {"closed": 2, "total": 2},
            "next_action": "none",
            "stop_reason": "plan_complete",
            "receipts": [{"path": "receipt.json"}] if receipts is None else receipts,
            "log_path": log_path,
        }

    def assert_continues(self, session="session-a", hooks=None):
        directive = (hooks or self.hooks).pre_verify(
            session_id=session, coding=True, changed_paths=[str(self.changed)]
        )
        self.assertIsInstance(directive, dict)
        self.assertEqual("continue", directive["action"])
        return directive

    def test_status_only_never_allows_completion(self):
        self.bind()
        self.hooks.record_status(str(self.root), self.result(action="status"), session_id="session-a")
        self.assertIn("not verify", self.assert_continues()["message"])

    def test_terminal_output_enforcement_rewrites_status_only_success_claim(self):
        """Hermes 0.21.3 skips pre_verify when a turn has no recorded code edits."""
        self.bind()
        self.hooks.record_status(str(self.root), self.result(action="status"), session_id="session-a")
        blocked = self.hooks.transform_llm_output(
            session_id="session-a", response_text="Done", turn_id="turn-1"
        )
        self.assertIsInstance(blocked, str)
        self.assertTrue(blocked.startswith("BLOCKED:"))
        self.assertIn("not verify", blocked)

    def test_terminal_output_enforcement_allows_same_session_verify_only(self):
        self.bind("session-a")
        self.bind("session-b")
        self.hooks.record_status(str(self.root), self.result(), session_id="session-a")
        self.assertIsNone(self.hooks.transform_llm_output(session_id="session-a", response_text="Done"))
        self.assertIn(
            "missing",
            self.hooks.transform_llm_output(session_id="session-b", response_text="Done"),
        )

    def test_nonzero_verify_exit_codes_fail_closed(self):
        self.bind()
        for code in (1, 2):
            with self.subTest(exit_code=code):
                self.hooks.record_status(str(self.root), self.result(exit_code=code, outcome="error"), session_id="session-a")
                self.assertIn("exit", self.assert_continues()["message"])

    def test_only_current_complete_verify_with_receipt_and_log_allows(self):
        self.bind()
        for override, text in (
            ({"outcome": "error"}, "did not complete"),
            ({"receipts": [], "evidence": None}, "no current receipt"),
            ({"receipts": [{}], "evidence": None}, "no current receipt"),
            ({"log_path": ""}, "no log_path"),
        ):
            with self.subTest(override=override):
                value = self.result()
                value.update(override)
                self.hooks.record_status(str(self.root), value, session_id="session-a")
                self.assertIn(text, self.assert_continues()["message"])
        self.hooks.record_status(str(self.root), self.result(), session_id="session-a")
        self.assertIsNone(self.hooks.pre_verify(session_id="session-a", changed_paths=[str(self.changed)]))

    def test_atomic_enforce_status_with_embedded_verify_allows_completion(self):
        self.bind()
        value = self.result(action="status")
        value.update({"verified": True, "verification_action": "verify"})
        self.hooks.record_status(str(self.root), value, session_id="session-a")
        self.assertIsNone(self.hooks.pre_verify(session_id="session-a", changed_paths=[str(self.changed)]))
        self.assertIsNone(self.hooks.transform_llm_output(session_id="session-a", response_text="Done"))

    def test_status_run_and_error_clear_prior_verification(self):
        self.bind()
        for action, outcome in (("status", "completed"), ("run", "completed"), ("verify", "error")):
            with self.subTest(action=action, outcome=outcome):
                self.hooks.record_status(str(self.root), self.result(), session_id="session-a")
                self.assertIsNone(self.hooks.pre_verify(session_id="session-a", changed_paths=[str(self.changed)]))
                replacement = self.result(action=action, outcome=outcome)
                self.hooks.record_status(str(self.root), replacement, session_id="session-a")
                self.assert_continues()

    def test_receipt_from_verify_becomes_stale_after_status(self):
        self.bind()
        self.hooks.record_status(str(self.root), self.result(), session_id="session-a")
        self.assertIsNone(self.hooks.pre_verify(session_id="session-a", changed_paths=[str(self.changed)]))
        self.hooks.record_status(str(self.root), self.result(action="status"), session_id="session-a")
        self.assertIn("not verify", self.assert_continues()["message"])

    def test_verification_does_not_cross_sessions(self):
        self.bind("session-a")
        self.bind("session-b")
        self.hooks.record_status(str(self.root), self.result(), session_id="session-a")
        self.assertIsNone(self.hooks.pre_verify(session_id="session-a", changed_paths=[str(self.changed)]))
        self.assertIn("missing", self.assert_continues("session-b")["message"])

    def test_plugin_restart_loses_evidence_and_fails_closed(self):
        self.bind()
        self.hooks.record_status(str(self.root), self.result(), session_id="session-a")
        self.assertIsNone(self.hooks.pre_verify(session_id="session-a", changed_paths=[str(self.changed)]))
        restarted = load_hooks()
        restarted.configure("airworker-hermes-v1-enforce")
        self.assertIn("missing", self.assert_continues(hooks=restarted)["message"])

    def test_turn_finalization_does_not_end_canonical_session(self):
        self.bind()
        self.hooks.record_status(str(self.root), self.result(), session_id="session-a")
        self.hooks.unbind_session(
            session_id="session-a", turn_id="turn-1", completed=True,
            failed=False, interrupted=False,
        )
        self.assertIsNone(self.hooks.pre_verify(session_id="session-a", changed_paths=[str(self.changed)]))
        self.hooks.unbind_session(session_id="session-a", reason="exit")
        self.assertIn("missing", self.assert_continues()["message"])

    def test_namespaced_tools_and_target_paths(self):
        self.bind()
        inside = str(self.root / "file.txt")
        outside = str(self.root.parent / "unrelated.txt")
        self.assertEqual(
            "block",
            self.hooks.pre_tool_call(session_id="session-a", tool_name="functions.terminal", args={"command": "dir"})["action"],
        )
        self.assertIsNone(
            self.hooks.pre_tool_call(session_id="session-a", tool_name="functions.read_file", args={"path": inside})
        )
        self.assertEqual(
            "block",
            self.hooks.pre_tool_call(session_id="session-a", tool_name="plugin__write_file", args={"path": inside})["action"],
        )
        self.assertIsNone(
            self.hooks.pre_tool_call(session_id="session-a", tool_name="plugin__write_file", args={"path": outside})
        )
        self.assertEqual(
            "block",
            self.hooks.pre_tool_call(session_id="session-a", tool_name="vendor.mystery_process", args={})["action"],
        )

    def test_shadow_and_unknown_profiles_observe_without_blocking(self):
        self.bind()
        for profile in ("airworker-hermes-v1-shadow", "", "custom-profile"):
            with self.subTest(profile=profile):
                self.hooks.configure(profile)
                self.assertIsNone(
                    self.hooks.pre_tool_call(session_id="session-a", tool_name="terminal", args={"command": "dir"})
                )
                self.assertIsNone(
                    self.hooks.pre_verify(session_id="session-a", changed_paths=[str(self.changed)])
                )
                self.assertIsNone(
                    self.hooks.transform_llm_output(session_id="session-a", response_text="Done")
                )
        self.assertIsNotNone(self.hooks.pre_llm_call(session_id="session-a"))


if __name__ == "__main__":
    unittest.main()
