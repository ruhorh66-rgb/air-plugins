from __future__ import annotations

import importlib.util
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "skills" / "route-coding-task" / "scripts" / "run_coding_task.py"
spec = importlib.util.spec_from_file_location("aircoder_runner", RUNNER)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
sys.modules[spec.name] = module
spec.loader.exec_module(module)


def git(repo: Path, *args: str) -> str:
    proc = subprocess.run(["git", *args], cwd=repo, capture_output=True, text=True, check=True)
    return proc.stdout.strip()


def make_repo(base: Path) -> Path:
    repo = base / "repo"
    repo.mkdir()
    git(repo, "init")
    git(repo, "config", "user.email", "aircoder-test@example.invalid")
    git(repo, "config", "user.name", "AirCoder Test")
    (repo / "context.md").write_text("context\n", encoding="utf-8")
    (repo / "app.py").write_text("VALUE = 1\n", encoding="utf-8")
    (repo / "tests").mkdir()
    (repo / "tests" / "test_app.py").write_text("# protected\n", encoding="utf-8")
    git(repo, "add", ".")
    git(repo, "commit", "-m", "baseline")
    return repo


def make_task(repo: Path, task_id: str = "TEST-1") -> dict:
    return {
        "schema_version": 1,
        "task_id": task_id,
        "product": "fixture",
        "repo_root": str(repo),
        "expected_head": git(repo, "rev-parse", "HEAD"),
        "objective": "Make the requested fixture change.",
        "allowed_paths": ["app.py"],
        "protected_paths": ["tests"],
        "context_files": ["context.md"],
        "acceptance_commands": ["python -c \"print('ok')\""],
        "require_clean_start": True,
        "limits": {"max_repair_attempts": 2, "executor_timeout_seconds": 30, "check_timeout_seconds": 30},
    }


def save_task(path: Path, task: dict) -> Path:
    path.write_text(json.dumps(task, ensure_ascii=False, indent=2), encoding="utf-8")
    return path


def successful_executor(thread_id: str = "thread-1") -> dict:
    return {
        "returncode": 0,
        "timed_out": False,
        "elapsed_s": 0.1,
        "stdout": "",
        "stderr": "",
        "thread_id": thread_id,
        "usage": {"input_tokens": 10, "output_tokens": 2},
        "last_message": "done",
        "events": ["thread.started", "turn.completed"],
    }


def check_record(passed: bool) -> list[dict]:
    return [{
        "command": "fixture-check",
        "returncode": 0 if passed else 1,
        "passed": passed,
        "timed_out": False,
        "elapsed_s": 0.01,
        "stdout": "ok" if passed else "failed",
        "stderr": "",
    }]


class RunnerTests(unittest.TestCase):
    def test_parse_codex_events_captures_thread_and_usage(self) -> None:
        raw = "\n".join([
            json.dumps({"type": "thread.started", "thread_id": "abc"}),
            json.dumps({"type": "item.completed", "item": {"type": "agent_message", "text": "done"}}),
            json.dumps({"type": "turn.completed", "usage": {"input_tokens": 12, "output_tokens": 3}}),
        ])
        parsed = module.parse_codex_events(raw)
        self.assertEqual("abc", parsed["thread_id"])
        self.assertEqual("done", parsed["last_message"])
        self.assertEqual(12, parsed["usage"]["input_tokens"])

    def test_missing_context_blocks_before_executor(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            repo = make_repo(base)
            task = make_task(repo)
            task["context_files"] = ["missing.md"]
            ok, failures = module.check_context(task)
            self.assertFalse(ok)
            self.assertIn("context file missing: missing.md", failures)

    def test_out_of_scope_change_is_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            task = make_task(repo)
            (repo / "other.txt").write_text("outside\n", encoding="utf-8")
            changed = module.collect_changed_paths(repo)
            self.assertEqual(["other.txt"], module.scope_violations(task, changed))

    def test_protected_change_is_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            task = make_task(repo)
            (repo / "tests" / "test_app.py").write_text("changed\n", encoding="utf-8")
            changed = module.collect_changed_paths(repo)
            self.assertEqual(["tests/test_app.py"], module.protected_violations(task, changed))

    def test_dirty_start_is_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            task = make_task(repo)
            (repo / "app.py").write_text("VALUE = 2\n", encoding="utf-8")
            ok, failures = module.check_repo_identity(task)
            self.assertFalse(ok)
            self.assertTrue(any("working tree not clean" in item for item in failures))

    def test_repair_loop_is_bounded_to_two(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            repo = make_repo(base)
            task_path = save_task(base / "task.json", make_task(repo, "REPAIR-2"))
            run_root = base / "runs"
            with mock.patch.object(module, "invoke_codex", side_effect=[
                successful_executor("thread-x"), successful_executor("thread-x"), successful_executor("thread-x")
            ]) as invoke, mock.patch.object(module, "run_acceptance", return_value=check_record(False)):
                code, state = module.run_task(task_path, run_root, False)
            self.assertEqual(5, code)
            self.assertEqual("repair_limit_reached", state["status"])
            self.assertEqual(3, state["executor_attempts"])
            self.assertEqual(2, state["repair_attempts"])
            self.assertEqual(3, invoke.call_count)

    def test_executor_failure_is_not_paid_retry(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            repo = make_repo(base)
            task_path = save_task(base / "task.json", make_task(repo, "EXEC-FAIL"))
            failed = successful_executor()
            failed.update({"returncode": 1, "thread_id": None, "stderr": "tool unavailable"})
            with mock.patch.object(module, "invoke_codex", return_value=failed) as invoke:
                code, state = module.run_task(task_path, base / "runs", False)
            self.assertEqual(3, code)
            self.assertEqual("executor_failed", state["status"])
            self.assertEqual(1, invoke.call_count)

    def test_usage_limit_is_executor_unavailable_without_retry(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            repo = make_repo(base)
            task_path = save_task(base / "task.json", make_task(repo, "QUOTA"))
            message = "You've hit your usage limit. Try again at 5:46 PM."
            failed = successful_executor("thread-quota")
            failed.update({
                "returncode": 1,
                "stdout": "\n".join([
                    json.dumps({"type": "thread.started", "thread_id": "thread-quota"}),
                    json.dumps({"type": "error", "message": message}),
                    json.dumps({"type": "turn.failed", "error": {"message": message}}),
                ]),
                "stderr": "Reading additional input from stdin...\n",
            })
            with mock.patch.object(module, "invoke_codex", return_value=failed) as invoke:
                code, state = module.run_task(task_path, base / "runs", False)
            self.assertEqual(3, code)
            self.assertEqual("executor_unavailable", state["status"])
            self.assertEqual("executor_unavailable", state["failure"]["kind"])
            self.assertEqual("usage_limit", state["failure"]["reason"])
            self.assertEqual("5:46 PM", state["failure"]["retry_after_hint"])
            self.assertEqual(1, invoke.call_count)

    def test_inflight_resume_refuses_duplicate_execution(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            repo = make_repo(base)
            task_path = save_task(base / "task.json", make_task(repo, "INFLIGHT"))
            task, state, state_path = module.initialize_run(task_path, base / "runs")
            module.save_state(state_path, state, "executor_running")
            with mock.patch.object(module, "invoke_codex") as invoke:
                code, resumed = module.run_task(task_path, base / "runs", True)
            self.assertEqual(2, code)
            self.assertEqual("uncertain_inflight", resumed["status"])
            invoke.assert_not_called()

    def test_accepted_resume_is_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            repo = make_repo(base)
            task_path = save_task(base / "task.json", make_task(repo, "DONE"))
            task, state, state_path = module.initialize_run(task_path, base / "runs")
            module.save_state(state_path, state, "accepted")
            with mock.patch.object(module, "invoke_codex") as invoke:
                code, resumed = module.run_task(task_path, base / "runs", True)
            self.assertEqual(0, code)
            self.assertEqual("accepted", resumed["status"])
            invoke.assert_not_called()


    def test_codex_args_keep_workspace_write_user_config(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            task = make_task(repo)
            with mock.patch.object(module, "codex_executable", return_value="codex.cmd"):
                initial = module.codex_args(task, "do")
                resumed = module.codex_args(task, "fix", "thread-1")
            self.assertIn("workspace-write", initial)
            self.assertNotIn("--ignore-user-config", initial)
            self.assertNotIn("--ignore-user-config", resumed)

    def test_invoke_codex_marks_child_as_leaf(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            task = make_task(repo)
            raw = "\n".join([
                json.dumps({"type": "thread.started", "thread_id": "leaf-thread"}),
                json.dumps({"type": "turn.completed", "usage": {"input_tokens": 1, "output_tokens": 1}}),
            ])

            fake = module.CommandResult("codex", 0, raw, "", 0.1)
            with mock.patch.object(module, "codex_args", return_value=["codex.cmd", "exec"]), \
                 mock.patch.object(module, "run_command", return_value=fake) as run:
                result = module.invoke_codex(task, "work", 30)
            self.assertEqual("leaf-thread", result["thread_id"])
            self.assertEqual("1", run.call_args.kwargs["env"][module.LEAF_ENV])

    def test_recursive_cli_is_blocked_in_leaf_mode(self) -> None:
        with mock.patch.dict(os.environ, {module.LEAF_ENV: "1"}), \
             mock.patch.object(module, "run_task") as run:
            self.assertEqual(2, module.main())
            run.assert_not_called()

    def test_internal_run_task_is_allowed_in_leaf_mode(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            repo = make_repo(base)
            task_path = save_task(base / "task.json", make_task(repo, "LEAF-TEST"))
            task, state, state_path = module.initialize_run(task_path, base / "runs")
            module.save_state(state_path, state, "accepted")
            with mock.patch.dict(os.environ, {module.LEAF_ENV: "1"}), \
                 mock.patch.object(module, "invoke_codex") as invoke:
                code, resumed = module.run_task(task_path, base / "runs", True)
            self.assertEqual(0, code)
            self.assertEqual("accepted", resumed["status"])
            invoke.assert_not_called()

    def test_context_prompt_declares_leaf_executor(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = make_repo(Path(tmp))
            prompt = module.context_prompt(make_task(repo))
            self.assertIn("leaf coding executor", prompt)
            self.assertIn("do not invoke AirCoder", prompt)
            self.assertIn("do not", prompt.lower())


if __name__ == "__main__":
    unittest.main()
