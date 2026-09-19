from __future__ import annotations

import importlib.util
import inspect
import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]


def load():
    name = "air_worker_plugin_test"
    spec = importlib.util.spec_from_file_location(name, ROOT / "__init__.py", submodule_search_locations=[str(ROOT)])
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    spec.loader.exec_module(mod)
    return mod


class Ctx:
    def __init__(self, profile_name=""):
        self.profile_name = profile_name
        self.tools = []
        self.hooks = []
        self.skills = []

    def register_tool(self, **kwargs):
        self.tools.append(kwargs)

    def register_hook(self, name, callback):
        self.hooks.append((name, callback))

    def register_skill(self, name, path):
        self.skills.append((name, Path(path)))


class Tests(unittest.TestCase):
    def setUp(self):
        self.p = load()
        self.p.hooks._reset_for_tests()

    @staticmethod
    def status(outcome="needs_action", **extra):
        value = {
            "schema_version": "air-worker.tool/v1",
            "action": "status",
            "outcome": outcome,
            "exit_code": 0,
            "progress": {"closed": 1, "total": 3},
            "next_action": "run_loop",
            "stop_reason": "no_live_worker",
            "receipts": [{"path": "r"}],
            "workers": [],
            "detail_path": "PLAN.md",
        }
        value.update(extra)
        return json.dumps(value)

    def test_contract(self):
        ctx = Ctx()
        self.p.register(ctx)
        self.assertEqual(["air_worker"], [item["name"] for item in ctx.tools])
        schema = ctx.tools[0]["schema"]["parameters"]
        self.assertEqual({"status", "run", "verify"}, set(schema["properties"]["action"]["enum"]))
        self.assertIn("config", schema["properties"])
        self.assertEqual(["action", "product"], schema["required"])
        self.assertFalse(schema["additionalProperties"])
        self.assertEqual(["operate-air-worker"], [item[0] for item in ctx.skills])
        self.assertTrue(ctx.skills[0][1].is_file())
        self.assertEqual(9, len(ctx.hooks))
        self.assertIn("transform_llm_output", [name for name, _ in ctx.hooks])
        for _, callback in ctx.hooks:
            self.assertIn("kwargs", inspect.signature(callback).parameters)

    def test_fixed_argv_config_and_private_logs(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            config = root / "cfg" / "run.json"
            config.parent.mkdir()
            config.write_text("{}", encoding="utf-8")
            calls = []

            def fake(argv, **kwargs):
                calls.append((list(argv), kwargs))
                stdout = self.status() if argv[1] == "adapter" else "detail"
                return type("Done", (), {"returncode": 0, "stdout": stdout, "stderr": ""})()

            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run", side_effect=fake):
                result = json.loads(self.p.adapter.execute({"action": "run", "product": str(root), "config": "cfg/run.json"}, session_id="s"))
            expected_tail = ["-product", str(root.resolve()), "-config", str(config.resolve()), "-principal", "hermes", "-session-key", "s"]
            self.assertEqual(["loop", *expected_tail], calls[0][0][1:])
            self.assertEqual(["adapter", "-action", "status", *expected_tail], calls[1][0][1:])
            self.assertTrue(all(call[1]["shell"] is False for call in calls))
            log = Path(result["log_path"])
            self.assertTrue(log.is_file())
            if os.name != "nt":
                self.assertEqual(0o600, stat.S_IMODE(log.stat().st_mode))
            binary.unlink()

    def test_config_is_passed_to_judge_and_cannot_escape_product(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            config = root / "run.json"
            config.write_text("{}", encoding="utf-8")
            done = type("Done", (), {"returncode": 0, "stdout": self.status("completed"), "stderr": ""})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run", return_value=done) as run:
                self.p.adapter.execute({"action": "verify", "product": str(root), "config": str(config)}, session_id="verify-session")
                rejected = json.loads(self.p.adapter.execute({"action": "status", "product": str(root), "config": "../outside.json"}))
            self.assertEqual("judge", run.call_args_list[0].args[0][1])
            self.assertEqual(str(config.resolve()), run.call_args_list[0].args[0][-1])
            self.assertNotIn("-principal", run.call_args_list[0].args[0])
            self.assertEqual(["-principal", "hermes", "-session-key", "verify-session"], run.call_args_list[1].args[0][-4:])
            self.assertEqual("config_invalid", rejected["stop_reason"])
            self.assertEqual(2, len(run.call_args_list))
            binary.unlink()

    def test_product_binary_is_never_discovered_or_executed(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            product_binary = root / "bin" / "air-worker.exe"
            product_binary.parent.mkdir()
            product_binary.write_bytes(b"hostile")
            env = {"AIR_WORKER_BIN": "", "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.shutil, "which", return_value=str(product_binary)), mock.patch.object(self.p.adapter.subprocess, "run") as run:
                result = json.loads(self.p.adapter.execute({"action": "status", "product": str(root)}))
            self.assertEqual("binary_not_found", result["stop_reason"])
            run.assert_not_called()

    def test_timeout_bytes_are_normalized_and_enveloped(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            timeout = subprocess.TimeoutExpired([str(binary), "loop"], 1, output=b"partial\xff", stderr=b"late\xfe")
            done = type("Done", (), {"returncode": 0, "stdout": self.status(), "stderr": ""})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run", side_effect=[timeout, done]):
                result = json.loads(self.p.adapter.execute({"action": "run", "product": str(root)}))
            self.assertEqual("air-worker.tool/v1", result["schema_version"])
            self.assertEqual(124, result["exit_code"])
            self.assertEqual("run_timeout", result["stop_reason"])
            json.loads(Path(result["log_path"]).read_text(encoding="utf-8"))
            binary.unlink()

    def test_zero_exit_run_with_pending_status_fails_continuity(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            loop = type("Done", (), {"returncode": 0, "stdout": "loop returned", "stderr": ""})()
            pending = type("Done", (), {"returncode": 0, "stdout": self.status(), "stderr": ""})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(
                self.p.adapter.subprocess, "run", side_effect=[loop, pending]
            ):
                result = json.loads(
                    self.p.adapter.execute({"action": "run", "product": str(root)}, session_id="s")
                )
            self.assertEqual("error", result["outcome"])
            self.assertEqual(2, result["exit_code"])
            self.assertEqual("continuity_violation", result["stop_reason"])
            self.assertEqual("run_loop", result["next_action"])
            binary.unlink()

    def test_launch_failure_is_enveloped(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run", side_effect=OSError("canary must not escape")), self.assertLogs(self.p.adapter._LOG, level="ERROR"):
                result = json.loads(self.p.adapter.execute({"action": "status", "product": str(root)}))
            self.assertEqual(126, result["exit_code"])
            self.assertEqual("launch_failed", result["stop_reason"])
            self.assertNotIn("canary", json.dumps(result).lower())
            binary.unlink()

    def test_log_failure_still_returns_valid_envelope(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            done = type("Done", (), {"returncode": 0, "stdout": self.status(), "stderr": ""})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run", return_value=done), mock.patch.object(self.p.adapter.os, "open", side_effect=PermissionError("no")), self.assertLogs(self.p.adapter._LOG, level="ERROR"):
                result = json.loads(self.p.adapter.execute({"action": "status", "product": str(root)}))
            self.assertEqual("air-worker.tool/v1", result["schema_version"])
            self.assertNotIn("log_path", result)
            binary.unlink()

    def test_secret_canary_is_redacted_from_result_and_log(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            canary = "canary-super-secret-12345"
            stdout = self.status(next_action=canary, detail_path="token=" + canary)
            done = type("Done", (), {"returncode": 0, "stdout": stdout, "stderr": "Authorization: Bearer " + canary})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs"), "TEST_API_TOKEN": canary}
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run", return_value=done):
                result_text = self.p.adapter.execute({"action": "status", "product": str(root)})
            result = json.loads(result_text)
            log_text = Path(result["log_path"]).read_text(encoding="utf-8")
            self.assertNotIn(canary, result_text)
            self.assertNotIn(canary, log_text)
            self.assertIn("[REDACTED]", result_text)
            self.assertIn("[REDACTED]", log_text)
            binary.unlink()

    def test_action_mapping_and_rejection(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            done = type("Done", (), {"returncode": 0, "stdout": self.status("completed"), "stderr": ""})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run", return_value=done) as run:
                self.p.adapter.execute({"action": "status", "product": str(root)})
                self.p.adapter.execute({"action": "verify", "product": str(root)})
                bad = json.loads(self.p.adapter.execute({"action": "shell", "product": str(root)}))
            self.assertEqual(["adapter", "judge", "adapter"], [item.args[0][1] for item in run.call_args_list])
            self.assertEqual("error", bad["outcome"])
            self.assertEqual("air-worker.tool/v1", bad["schema_version"])
            binary.unlink()

    def test_hooks(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            (root / "run-config.json").write_text("{}", encoding="utf-8")
            hooks = self.p.hooks
            hooks.configure("airworker-hermes-v1-enforce")
            self.assertIsNone(hooks.pre_tool_call(session_id="x", tool_name="terminal"))
            hooks.bind_session(session_id="s", cwd=str(root), future=True)
            self.assertLess(len(hooks.pre_llm_call(session_id="s")["context"]), 300)
            self.assertEqual("block", hooks.pre_tool_call(session_id="s", tool_name="terminal")["action"])
            self.assertIsNone(hooks.pre_tool_call(session_id="s", tool_name="air_worker"))
            self.assertEqual("continue", hooks.pre_verify(session_id="s")["action"])
            hooks.record_status(str(root), {"action": "status", "exit_code": 0, "outcome": "needs_action", "progress": {"closed": 1, "total": 2}, "next_action": "run_loop", "stop_reason": "no_live_worker", "receipts": []}, session_id="s")
            self.assertIn("not verify", hooks.pre_verify(session_id="s")["message"])
            hooks.record_status(str(root), {"action": "verify", "exit_code": 0, "outcome": "completed", "progress": {"closed": 2, "total": 2}, "next_action": "none", "stop_reason": "plan_complete", "receipts": [], "log_path": "verify.json"}, session_id="s")
            self.assertIn("no current receipt", hooks.pre_verify(session_id="s")["message"])
            hooks.record_status(str(root), {"action": "verify", "exit_code": 0, "outcome": "completed", "progress": {"closed": 2, "total": 2}, "next_action": "none", "stop_reason": "plan_complete", "receipts": [{"path": "r"}], "log_path": "verify.json"}, session_id="s")
            self.assertIsNone(hooks.pre_verify(session_id="s"))
            hooks.post_tool_call(session_id="s", tool_name="air_worker", result=json.dumps({"schema": "air-worker.tool/v1", "ok": True, "log_path": "l", "secret": "NO"}))
            hooks.subagent_start(session_id="s", subagent_id="c", prompt="NO")
            hooks.subagent_stop(session_id="s", subagent_id="c", result="NO")
            self.assertNotIn("NO", json.dumps(hooks.observations()))
            hooks.unbind_session(session_id="s")
            self.assertIsNone(hooks.pre_tool_call(session_id="s", tool_name="terminal"))

    def test_profile_modes_control_blocking_and_missing_identity(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            (root / "run-config.json").write_text("{}", encoding="utf-8")
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            done = type("Done", (), {"returncode": 0, "stdout": self.status(), "stderr": ""})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}

            shadow = Ctx("airworker-hermes-v1-shadow")
            self.p.register(shadow)
            self.p.hooks.bind_session(session_id="shadow-session", cwd=str(root))
            self.assertIsNone(self.p.hooks.pre_tool_call(session_id="shadow-session", tool_name="terminal"))
            self.assertIsNone(self.p.hooks.pre_verify(session_id="shadow-session"))
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run", return_value=done) as run:
                observed = json.loads(self.p.adapter.execute({"action": "status", "product": str(root)}))
            self.assertEqual("needs_action", observed["outcome"])
            self.assertEqual(1, run.call_count)

            enforce = Ctx("airworker-hermes-v1-enforce")
            self.p.register(enforce)
            self.p.hooks.bind_session(session_id="enforce-session", cwd=str(root))
            self.assertEqual("block", self.p.hooks.pre_tool_call(session_id="enforce-session", tool_name="terminal")["action"])
            self.assertEqual("continue", self.p.hooks.pre_verify(session_id="enforce-session")["action"])
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run") as run:
                for action in ("status", "run", "verify"):
                    with self.subTest(action=action):
                        rejected = json.loads(self.p.adapter.execute({"action": action, "product": str(root)}))
                        self.assertEqual("session_id_required", rejected["stop_reason"])
                run.assert_not_called()
            binary.unlink()

    def test_enforce_status_promotes_orphaned_work_to_loop(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            self.p.register(Ctx("airworker-hermes-v1-enforce"))
            pending = type("Done", (), {"returncode": 0, "stdout": self.status(), "stderr": ""})()
            loop = type("Done", (), {"returncode": 0, "stdout": "loop complete", "stderr": ""})()
            complete = type("Done", (), {
                "returncode": 0,
                "stdout": self.status(
                    "completed", progress={"closed": 3, "total": 3},
                    next_action="none", stop_reason="plan_complete"
                ),
                "stderr": "",
            })()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(
                self.p.adapter.subprocess, "run", side_effect=[pending, loop, complete, loop, complete]
            ) as run:
                result = json.loads(
                    self.p.adapter.execute({"action": "status", "product": str(root)}, session_id="s")
                )
            self.assertEqual(["adapter", "loop", "adapter", "judge", "adapter"], [item.args[0][1] for item in run.call_args_list])
            self.assertEqual("completed", result["outcome"])
            self.assertEqual(0, result["exit_code"])
            self.assertTrue(result["verified"])
            self.assertEqual("verify", result["verification_action"])
            for call in run.call_args_list:
                if call.args[0][1] in {"adapter", "loop"}:
                    self.assertEqual(["-principal", "hermes", "-session-key", "s"], call.args[0][-4:])
            binary.unlink()

    def test_enforce_status_does_not_promote_terminal_or_running_states(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            self.p.register(Ctx("airworker-hermes-v1-enforce"))
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            states = (
                self.status("completed", progress={"closed": 3, "total": 3}, next_action="none", stop_reason="plan_complete"),
                self.status("waiting", next_action="approve_lpr", stop_reason="lpr_gate"),
                self.status("running", next_action="wait", stop_reason="", workers=[{"pid": 42}]),
            )
            for index, stdout in enumerate(states):
                with self.subTest(index=index), mock.patch.dict(os.environ, env), mock.patch.object(
                    self.p.adapter.subprocess, "run",
                    return_value=type("Done", (), {"returncode": 0, "stdout": stdout, "stderr": ""})(),
                ) as run:
                    result = json.loads(self.p.adapter.execute({"action": "status", "product": str(root)}, session_id="s"))
                    self.assertEqual(3 if index == 0 else 1, run.call_count)
                    if index == 0:
                        self.assertTrue(result["verified"])
            binary.unlink()

    def test_enforce_auto_verify_failure_is_structured(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            self.p.register(Ctx("airworker-hermes-v1-enforce"))
            pending = type("Done", (), {"returncode": 0, "stdout": self.status(), "stderr": ""})()
            done = type("Done", (), {"returncode": 0, "stdout": "done", "stderr": ""})()
            complete = type("Done", (), {
                "returncode": 0,
                "stdout": self.status("completed", progress={"closed": 2, "total": 2}, next_action="none", stop_reason="plan_complete"),
                "stderr": "",
            })()
            judge_failed = type("Done", (), {"returncode": 5, "stdout": "", "stderr": "not proved"})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(
                self.p.adapter.subprocess, "run", side_effect=[pending, done, complete, judge_failed, complete]
            ):
                result = json.loads(self.p.adapter.execute({"action": "status", "product": str(root)}, session_id="s"))
            self.assertEqual("error", result["outcome"])
            self.assertEqual(5, result["exit_code"])
            self.assertEqual("verify_exit_5", result["stop_reason"])
            self.assertNotIn("verified", result)
            binary.unlink()

    def test_enforce_promoted_loop_failure_is_structured(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            self.p.register(Ctx("airworker-hermes-v1-enforce"))
            pending = type("Done", (), {"returncode": 0, "stdout": self.status(), "stderr": ""})()
            failed = type("Done", (), {"returncode": 7, "stdout": "", "stderr": "failed"})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(
                self.p.adapter.subprocess, "run", side_effect=[pending, failed, pending]
            ):
                result = json.loads(
                    self.p.adapter.execute({"action": "status", "product": str(root)}, session_id="s")
                )
            self.assertEqual("error", result["outcome"])
            self.assertEqual(7, result["exit_code"])
            self.assertEqual("run_exit_7", result["stop_reason"])
            binary.unlink()

    def test_receipt_limit_is_explicit_and_omitted_count_is_preserved(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            binary = root.parent / (root.name + "-trusted-air-worker")
            binary.write_bytes(b"x")
            receipts = [{"path": f"receipt-{index}.json"} for index in range(12)]
            done = type("Done", (), {"returncode": 0, "stdout": self.status(receipts=receipts, receipts_omitted=4), "stderr": ""})()
            env = {"AIR_WORKER_BIN": str(binary.resolve()), "AIR_WORKER_LOG_DIR": str(root / "logs")}
            with mock.patch.dict(os.environ, env), mock.patch.object(self.p.adapter.subprocess, "run", return_value=done):
                result = json.loads(self.p.adapter.execute({"action": "status", "product": str(root)}, session_id="s"))
            self.assertEqual(8, len(result["receipts"]))
            self.assertEqual(4, result["receipts_omitted"])
            binary.unlink()


if __name__ == "__main__":
    unittest.main()
