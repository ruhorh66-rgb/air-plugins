"""Synthetic-only regression: native Claude judge must enforce read-only tools."""
from __future__ import annotations

import importlib.util
from pathlib import Path
import sys
from types import SimpleNamespace

import pytest

HELPER = Path(__file__).resolve().parents[1] / "scripts" / "claude_judge_run.py"
assert HELPER.is_file()
spec = importlib.util.spec_from_file_location("airworker_readonly_claude_judge", HELPER)
assert spec and spec.loader
judge = importlib.util.module_from_spec(spec)
spec.loader.exec_module(judge)


def assert_readonly(argv):
    assert argv[:4] == ["synthetic-claude.exe", "-p", "--model", "claude-sonnet-5-5"]
    assert argv[argv.index("--output-format") + 1] == "json"
    assert argv[argv.index("--tools") + 1] == "Read,Grep,Glob"
    assert argv[argv.index("--permission-mode") + 1] == "plan"
    assert argv[argv.index("--permission-prompts") + 1] == "none"
    for option in ("--restricted", "--strict-mcp-config", "--no-session-persistence"):
        assert option in argv
    deny = set(argv[argv.index("--disallowedTools") + 1].split(","))
    assert {"Write", "Edit", "Bash", "PowerShell", "Task", "mcp__*"} <= deny
    assert len(argv) == len(set(argv))
    assert not any("dangerously" in a.lower() or "bypass" in a.lower() for a in argv)


def test_native_readonly_argv_strict():
    assert_readonly(judge._readonly_claude_argv("synthetic-claude.exe", "claude-sonnet-5-5"))


def test_utf8_prompt_stdin_no_secret_in_args_or_log(monkeypatch, tmp_path, capsys):
    prompt = tmp_path / "utf8_task.txt"
    prompt.write_text("Проверка кода только чтение\n", encoding="utf-8")
    token = "SYNTHETIC_TOKEN_NOT_REAL"
    env = {"CLAUDE_CODE_OAUTH_TOKEN": token, "ANTHROPIC_BASE_URL": "http://localhost:8787"}
    invocations = []

    def mock_process(args, **kwargs):
        invocations.append((args, kwargs))
        return SimpleNamespace(returncode=0, stdout='{"result":"SYNTHETIC"}\n', stderr="")

    monkeypatch.setattr(judge, "_resolve_claude_exe", lambda: "synthetic-claude.exe")
    monkeypatch.setattr(judge, "_environment_with_token", lambda: dict(env))
    monkeypatch.setattr(judge.subprocess, "run", mock_process)
    monkeypatch.setattr(sys, "argv", ["helper.py", "claude-sonnet-5-5", str(prompt)])
    assert judge.main() == 0
    assert len(invocations) == 1
    args, opts = invocations[0]
    assert_readonly(args)
    assert opts["input"] == "Проверка кода только чтение\n"
    assert opts["env"] == env and opts["capture_output"] is True
    assert opts["encoding"] == "utf-8" and opts["timeout"] == 300
    assert opts.get("shell") is None
    out = capsys.readouterr()
    assert "SYNTHETIC" in out.out
    assert token not in out.out + out.err + " ".join(args)


@pytest.mark.parametrize("code", [1, 2, 3, 17])
def test_unsupported_readonly_flags_fail_closed_never_retry(monkeypatch, tmp_path, capsys, code):
    task = tmp_path / "test.txt"
    task.write_text("synthetic", encoding="utf-8")
    calls = []

    def failed(argv, **kwargs):
        calls.append(argv)
        return SimpleNamespace(returncode=code, stdout="", stderr="Synthetic Claude refused argv")

    monkeypatch.setattr(judge, "_resolve_claude_exe", lambda: "synthetic-claude.exe")
    monkeypatch.setattr(judge, "_environment_with_token", lambda: {})
    monkeypatch.setattr(judge.subprocess, "run", failed)
    monkeypatch.setattr(sys, "argv", ["helper.py", "claude-sonnet-5-5", str(task)])
    assert judge.main() == code
    assert len(calls) == 1
    assert_readonly(calls[0])
    assert "Synthetic Claude refused" in capsys.readouterr().err


def test_missing_binary_refuses_launch(monkeypatch, tmp_path, capsys):
    task = tmp_path / "test.txt"
    task.write_text("synthetic", encoding="utf-8")

    def not_found():
        raise FileNotFoundError("synthetic binary absent")

    monkeypatch.setattr(judge, "_resolve_claude_exe", not_found)
    monkeypatch.setattr(judge.subprocess, "run", lambda *_args, **_kwargs: pytest.fail("launched"))
    monkeypatch.setattr(sys, "argv", ["helper.py", "claude-sonnet-5-5", str(task)])
    assert judge.main() == 2
    assert "synthetic binary absent" in capsys.readouterr().err


def test_two_argument_contract_refuses_extra_or_missing(monkeypatch):
    monkeypatch.setattr(judge.subprocess, "run", lambda *_args, **_kwargs: pytest.fail("launched"))
    for argv in (["helper.py"], ["helper.py", "claude-sonnet-5-5"], ["helper.py", "claude-sonnet-5-5", "path", "extra"]):
        monkeypatch.setattr(sys, "argv", argv)
        assert judge.main() == 2
