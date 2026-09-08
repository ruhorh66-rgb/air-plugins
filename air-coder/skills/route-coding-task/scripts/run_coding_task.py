from __future__ import annotations

import argparse
import hashlib
import json
import os
import shlex
import shutil
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable

DEFAULT_RUN_ROOT = Path(r"E:\-4-\air-coder\runs")
FINAL_STATES = {
    "accepted",
    "blocked_context",
    "blocked_scope",
    "blocked_protected",
    "executor_failed",
    "executor_unavailable",
    "repair_limit_reached",
    "uncertain_inflight",
}
INFLIGHT_STATES = {"executor_running", "repair_running"}
MAX_OUTPUT_CHARS = 12000
LEAF_ENV = "AIR_CODER_LEAF_ACTIVE"


class ContractError(ValueError):
    pass


@dataclass
class CommandResult:
    command: str
    returncode: int
    stdout: str
    stderr: str
    elapsed_s: float
    timed_out: bool = False

    @property
    def passed(self) -> bool:
        return self.returncode == 0 and not self.timed_out


def utc_now() -> str:
    return __import__("datetime").datetime.now(__import__("datetime").timezone.utc).isoformat()


def truncate(text: str, limit: int = MAX_OUTPUT_CHARS) -> str:
    return text if len(text) <= limit else text[:limit] + "\n...[truncated]"


def read_json(path: Path) -> dict[str, Any]:
    try:
        data = json.loads(path.read_text(encoding="utf-8-sig"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ContractError(f"cannot read JSON {path}: {exc}") from exc
    if not isinstance(data, dict):
        raise ContractError(f"JSON root must be an object: {path}")
    return data


def write_json(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    os.replace(tmp, path)


def require_text(task: dict[str, Any], key: str) -> str:
    value = task.get(key)
    if not isinstance(value, str) or not value.strip():
        raise ContractError(f"{key} must be a non-empty string")
    return value.strip()


def require_string_list(task: dict[str, Any], key: str) -> list[str]:
    value = task.get(key)
    if not isinstance(value, list) or not value:
        raise ContractError(f"{key} must be a non-empty list")
    if any(not isinstance(item, str) for item in value):
        raise ContractError(f"{key} must contain only strings")
    items = [item.strip() for item in value]
    if any(not item for item in items):
        raise ContractError(f"{key} contains an empty item")
    return items


TASK_KEYS = {
    "schema_version", "task_id", "product", "repo_root", "expected_remote",
    "expected_head", "objective", "allowed_paths", "protected_paths",
    "context_files", "acceptance_commands", "require_clean_start",
    "scarce_quota_burden", "executor", "limits",
}

def validate_task(task: dict[str, Any]) -> None:
    unknown = sorted(set(task) - TASK_KEYS)
    if unknown:
        raise ContractError("unknown task fields: " + ", ".join(unknown))
    if task.get("schema_version") != 1:
        raise ContractError("schema_version must be 1")
    for key in ("task_id", "product", "repo_root", "objective"):
        require_text(task, key)
    for key in ("allowed_paths", "context_files", "acceptance_commands"):
        require_string_list(task, key)
    protected = task.get("protected_paths", [])
    if (
        not isinstance(protected, list)
        or any(not isinstance(item, str) or not item.strip() for item in protected)
    ):
        raise ContractError("protected_paths must be a list of non-empty strings")
    for key in ("expected_remote", "expected_head"):
        value = task.get(key)
        if value is not None and not isinstance(value, str):
            raise ContractError(f"{key} must be a string or null")
    if "require_clean_start" in task and not isinstance(task["require_clean_start"], bool):
        raise ContractError("require_clean_start must be boolean")
    if "executor" in task and not isinstance(task["executor"], dict):
        raise ContractError("executor must be an object")
    if "limits" in task and not isinstance(task["limits"], dict):
        raise ContractError("limits must be an object")
    quota = task.get("scarce_quota_burden", "medium")
    if quota not in {"low", "medium", "high"}:
        raise ContractError("scarce_quota_burden must be one of: low, medium, high")


def run_command(
    command: str | list[str],
    cwd: Path,
    timeout_s: int,
    *,
    shell: bool = False,
    env: dict[str, str] | None = None,
) -> CommandResult:
    started = time.monotonic()
    printable = command if isinstance(command, str) else subprocess.list2cmdline(command)
    try:
        proc = subprocess.run(
            command,
            cwd=str(cwd),
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout_s,
            shell=shell,
            env=env,
        )
        return CommandResult(
            printable,
            proc.returncode,
            truncate(proc.stdout),
            truncate(proc.stderr),
            round(time.monotonic() - started, 3),
        )
    except subprocess.TimeoutExpired as exc:
        stdout = exc.stdout.decode("utf-8", "replace") if isinstance(exc.stdout, bytes) else (exc.stdout or "")
        stderr = exc.stderr.decode("utf-8", "replace") if isinstance(exc.stderr, bytes) else (exc.stderr or "")
        return CommandResult(
            printable,
            124,
            truncate(stdout),
            truncate(stderr),
            round(time.monotonic() - started, 3),
            timed_out=True,
        )


def git(repo: Path, *args: str, timeout_s: int = 30) -> CommandResult:
    return run_command(["git", *args], repo, timeout_s)


def normalize_rel(path: str) -> str:
    value = path.replace("\\", "/").strip()
    while value.startswith("./"):
        value = value[2:]
    return value.rstrip("/")


def path_matches(path: str, rules: Iterable[str]) -> bool:
    candidate = normalize_rel(path)
    for raw_rule in rules:
        rule = normalize_rel(raw_rule)
        if candidate == rule or candidate.startswith(rule + "/"):
            return True
    return False


def collect_changed_paths(repo: Path) -> list[str]:
    paths: set[str] = set()
    commands = [
        ("diff", "--name-only"),
        ("diff", "--cached", "--name-only"),
        ("ls-files", "--others", "--exclude-standard"),
    ]
    for args in commands:
        result = git(repo, *args)
        if not result.passed:
            raise RuntimeError(f"git {' '.join(args)} failed: {result.stderr or result.stdout}")
        paths.update(normalize_rel(line) for line in result.stdout.splitlines() if line.strip())
    return sorted(paths)


def resolve_context_path(repo: Path, value: str) -> Path:
    candidate = Path(value)
    return candidate if candidate.is_absolute() else repo / candidate


def check_context(task: dict[str, Any]) -> tuple[bool, list[str]]:
    repo = Path(require_text(task, "repo_root")).resolve()
    failures: list[str] = []
    if not repo.is_dir():
        return False, [f"repo_root missing: {repo}"]
    top = git(repo, "rev-parse", "--show-toplevel")
    if not top.passed or Path(top.stdout.strip()).resolve() != repo:
        failures.append(f"repo_root mismatch: expected {repo}; git={top.stdout.strip() or top.stderr.strip()}")
    for value in require_string_list(task, "context_files"):
        if not resolve_context_path(repo, value).is_file():
            failures.append(f"context file missing: {value}")
    expected_head = task.get("expected_head")
    if expected_head:
        head = git(repo, "rev-parse", "HEAD")
        if not head.passed or head.stdout.strip() != str(expected_head):
            failures.append(f"HEAD mismatch: expected {expected_head}; actual {head.stdout.strip()}")
    return not failures, failures


def check_repo_identity(task: dict[str, Any], *, check_clean: bool | None = None) -> tuple[bool, list[str]]:
    repo = Path(require_text(task, "repo_root")).resolve()
    failures: list[str] = []
    expected_remote = task.get("expected_remote")
    if expected_remote:
        remote = git(repo, "remote", "get-url", "origin")
        actual = remote.stdout.strip().rstrip("/") if remote.passed else ""
        expected = str(expected_remote).strip().rstrip("/")
        if actual != expected:
            failures.append(f"origin mismatch: expected {expected}; actual {actual or remote.stderr.strip()}")
    require_clean = bool(task.get("require_clean_start", True)) if check_clean is None else check_clean
    if require_clean:
        changed = collect_changed_paths(repo)
        if changed:
            failures.append("working tree not clean at task start: " + ", ".join(changed))
    return not failures, failures


def protected_violations(task: dict[str, Any], changed: Iterable[str]) -> list[str]:
    rules = [str(item) for item in task.get("protected_paths", [])]
    return sorted(path for path in changed if path_matches(path, rules))


def scope_violations(task: dict[str, Any], changed: Iterable[str]) -> list[str]:
    allowed = require_string_list(task, "allowed_paths")
    return sorted(path for path in changed if not path_matches(path, allowed))


def run_acceptance(task: dict[str, Any], timeout_s: int) -> list[dict[str, Any]]:
    repo = Path(require_text(task, "repo_root")).resolve()
    records: list[dict[str, Any]] = []
    commands = ["git diff HEAD --check", *require_string_list(task, "acceptance_commands")]
    for command in commands:
        result = run_command(command, repo, timeout_s, shell=True)
        records.append({
            "command": result.command,
            "returncode": result.returncode,
            "passed": result.passed,
            "timed_out": result.timed_out,
            "elapsed_s": result.elapsed_s,
            "stdout": result.stdout,
            "stderr": result.stderr,
        })
        if not result.passed:
            break
    return records


def acceptance_passed(records: list[dict[str, Any]]) -> bool:
    return bool(records) and all(bool(item["passed"]) for item in records)


def codex_executable() -> str:
    for name in ("codex.cmd", "codex"):
        found = shutil.which(name)
        if found:
            return found
    raise RuntimeError("Codex CLI not found in PATH")


def codex_args(task: dict[str, Any], prompt: str, thread_id: str | None = None) -> list[str]:
    executor = task.get("executor", {}) if isinstance(task.get("executor", {}), dict) else {}
    args = [codex_executable(), "exec"]
    if thread_id:
        args += ["resume", "--json"]
        model = executor.get("model")
        if model:
            args += ["-m", str(model)]
        effort = executor.get("reasoning_effort")
        if effort:
            args += ["-c", f'model_reasoning_effort="{effort}"']
        args += [thread_id, prompt]
        return args
    args += ["--json", "-s", str(executor.get("sandbox", "workspace-write"))]
    args += ["-C", str(Path(require_text(task, "repo_root")).resolve())]
    model = executor.get("model")
    if model:
        args += ["-m", str(model)]
    effort = executor.get("reasoning_effort")
    if effort:
        args += ["-c", f'model_reasoning_effort="{effort}"']
    args.append(prompt)
    return args


def parse_codex_events(stdout: str) -> dict[str, Any]:
    parsed: dict[str, Any] = {"thread_id": None, "usage": None, "last_message": None, "events": []}
    for line in stdout.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(event, dict):
            continue
        parsed["events"].append(event.get("type"))
        if event.get("type") == "thread.started":
            parsed["thread_id"] = event.get("thread_id")
        elif event.get("type") == "turn.completed":
            parsed["usage"] = event.get("usage")
        elif event.get("type") == "item.completed":
            item = event.get("item", {})
            if isinstance(item, dict) and item.get("type") == "agent_message":
                parsed["last_message"] = item.get("text")
    return parsed


def context_prompt(task: dict[str, Any]) -> str:
    allowed = "\n".join(f"- {item}" for item in require_string_list(task, "allowed_paths"))
    protected = "\n".join(f"- {item}" for item in task.get("protected_paths", [])) or "- none"
    context = "\n".join(f"- {item}" for item in require_string_list(task, "context_files"))
    return (
        f"AirCoder task {task['task_id']} for product {task['product']}.\n"
        f"Objective:\n{task['objective']}\n\n"
        f"Read these context files before editing:\n{context}\n\n"
        f"You may modify only these paths:\n{allowed}\n\n"
        f"Protected paths; do not modify them:\n{protected}\n\n"
        "You are a leaf coding executor. Work directly on the requested code; do not invoke AirCoder, "
        "select or route another executor, spawn or delegate to agents, create another task contract, or call run_coding_task.py. "
        "Do not commit, push, merge, tag, release, or change runtime outside this repository. "
        "Make the smallest sufficient code change. Repository checks are run independently after you finish."
    )


def repair_prompt(task: dict[str, Any], failures: list[dict[str, Any]], attempt: int) -> str:
    details = []
    for record in failures:
        details.append(
            f"COMMAND: {record['command']}\nRC: {record['returncode']}\n"
            f"STDOUT:\n{record['stdout']}\nSTDERR:\n{record['stderr']}"
        )
    return (
        f"AirCoder repair attempt {attempt}. Independent acceptance failed.\n\n"
        + "\n\n".join(details)
        + "\n\nFix only the product defect within the original allowed paths. "
        "Do not weaken or edit protected tests/configuration. Do not commit or push."
    )


def invoke_codex(task: dict[str, Any], prompt: str, timeout_s: int, thread_id: str | None = None) -> dict[str, Any]:
    repo = Path(require_text(task, "repo_root")).resolve()
    child_env = os.environ.copy()
    child_env[LEAF_ENV] = "1"
    result = run_command(codex_args(task, prompt, thread_id), repo, timeout_s, env=child_env)
    parsed = parse_codex_events(result.stdout)
    return {
        "returncode": result.returncode,
        "timed_out": result.timed_out,
        "elapsed_s": result.elapsed_s,
        "stdout": result.stdout,
        "stderr": result.stderr,
        "thread_id": parsed["thread_id"] or thread_id,
        "usage": parsed["usage"],
        "last_message": parsed["last_message"],
        "events": parsed["events"],
    }


def failed_checks(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    return [record for record in records if not bool(record["passed"])]


def limits(task: dict[str, Any]) -> tuple[int, int, int]:
    raw = task.get("limits", {}) if isinstance(task.get("limits", {}), dict) else {}
    repairs = int(raw.get("max_repair_attempts", 2))
    if repairs < 0 or repairs > 2:
        raise ContractError("max_repair_attempts must be between 0 and 2")
    executor_timeout = int(raw.get("executor_timeout_seconds", 900))
    check_timeout = int(raw.get("check_timeout_seconds", 300))
    return repairs, executor_timeout, check_timeout


def run_root_from_args(value: str | None) -> Path:
    if value:
        return Path(value).resolve()
    env_value = os.environ.get("AIR_CODER_RUN_ROOT")
    return Path(env_value).resolve() if env_value else DEFAULT_RUN_ROOT


def new_state(task: dict[str, Any], task_path: Path, run_dir: Path) -> dict[str, Any]:
    created_at = utc_now()
    return {
        "schema_version": 1,
        "task_id": task["task_id"],
        "product": task["product"],
        "status": "created",
        "created_at": created_at,
        "updated_at": created_at,
        "task_contract": str(task_path.resolve()),
        "run_dir": str(run_dir),
        "thread_id": None,
        "executor_attempts": 0,
        "repair_attempts": 0,
        "executor_runs": [],
        "acceptance_runs": [],
        "changed_paths": [],
        "failure": None,
        "result": None,
        "timing": {
            "active_elapsed_s": 0.0,
            "resume_wait_elapsed_s": 0.0,
            "resume_count": 0,
            "calendar_started_at": created_at,
            "calendar_completed_at": None,
        },
    }


def save_state(path: Path, state: dict[str, Any], status: str | None = None) -> None:
    if status:
        state["status"] = status
    state["updated_at"] = utc_now()
    write_json(path, state)


def gate_changed_paths(task: dict[str, Any], state: dict[str, Any], state_path: Path) -> bool:
    repo = Path(require_text(task, "repo_root")).resolve()
    changed = collect_changed_paths(repo)
    state["changed_paths"] = changed
    protected = protected_violations(task, changed)
    if protected:
        state["failure"] = {"kind": "protected_path_modified", "paths": protected}
        save_state(state_path, state, "blocked_protected")
        return False
    outside = scope_violations(task, changed)
    if outside:
        state["failure"] = {"kind": "out_of_scope_diff", "paths": outside}
        save_state(state_path, state, "blocked_scope")
        return False
    return True


def executor_failed(run: dict[str, Any]) -> bool:
    return bool(run["timed_out"]) or int(run["returncode"]) != 0 or not run.get("thread_id")


def executor_unavailability(run: dict[str, Any]) -> dict[str, Any] | None:
    messages: list[str] = []
    for line in str(run.get("stdout") or "").splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(event, dict):
            continue
        if event.get("type") == "error" and event.get("message"):
            messages.append(str(event["message"]))
        elif event.get("type") == "turn.failed":
            error = event.get("error")
            if isinstance(error, dict) and error.get("message"):
                messages.append(str(error["message"]))
    text = "\n".join(messages)
    if "usage limit" not in text.lower():
        return None
    hint = None
    marker = "try again at "
    lower = text.lower()
    pos = lower.find(marker)
    if pos >= 0:
        raw = text[pos + len(marker):].splitlines()[0].strip()
        hint = raw.rstrip(". ") or None
    return {"reason": "usage_limit", "message": messages[0] if messages else "usage limit", "retry_after_hint": hint}


def execute_one_turn(
    task: dict[str, Any],
    state: dict[str, Any],
    state_path: Path,
    prompt: str,
    timeout_s: int,
    *,
    repair: bool,
) -> bool:
    save_state(state_path, state, "repair_running" if repair else "executor_running")
    run = invoke_codex(task, prompt, timeout_s, state.get("thread_id") if repair else None)
    state["executor_attempts"] += 1
    if repair:
        state["repair_attempts"] += 1
    state["thread_id"] = run.get("thread_id")
    state["executor_runs"].append(run)
    add_active_elapsed(state, float(run.get("elapsed_s") or 0.0))
    if executor_failed(run):
        unavailable = executor_unavailability(run)
        if unavailable:
            state["failure"] = {
                "kind": "executor_unavailable",
                **unavailable,
                "returncode": run["returncode"],
                "timed_out": run["timed_out"],
            }
            save_state(state_path, state, "executor_unavailable")
            return False
        state["failure"] = {
            "kind": "executor_failure",
            "returncode": run["returncode"],
            "timed_out": run["timed_out"],
            "stderr": run["stderr"],
        }
        save_state(state_path, state, "executor_failed")
        return False
    save_state(state_path, state, "executor_completed")
    return True


def ensure_timing(state: dict[str, Any]) -> dict[str, Any]:
    timing = state.setdefault("timing", {})
    timing.setdefault("active_elapsed_s", 0.0)
    timing.setdefault("resume_wait_elapsed_s", 0.0)
    timing.setdefault("resume_count", 0)
    timing.setdefault("calendar_started_at", state.get("created_at"))
    timing.setdefault("calendar_completed_at", None)
    return timing


def add_active_elapsed(state: dict[str, Any], elapsed_s: float) -> None:
    timing = ensure_timing(state)
    timing["active_elapsed_s"] = round(float(timing["active_elapsed_s"]) + max(0.0, elapsed_s), 3)


def record_acceptance_elapsed(state: dict[str, Any], records: list[dict[str, Any]]) -> None:
    add_active_elapsed(state, sum(float(record.get("elapsed_s") or 0.0) for record in records))


def record_resume_wait(state: dict[str, Any]) -> None:
    timing = ensure_timing(state)
    raw = state.get("updated_at")
    if isinstance(raw, str):
        try:
            dt = __import__("datetime").datetime.fromisoformat(raw)
            now = __import__("datetime").datetime.now(__import__("datetime").timezone.utc)
            if dt.tzinfo is None:
                dt = dt.replace(tzinfo=__import__("datetime").timezone.utc)
            timing["resume_wait_elapsed_s"] = round(float(timing["resume_wait_elapsed_s"]) + max(0.0, (now - dt).total_seconds()), 3)
        except ValueError:
            pass
    timing["resume_count"] = int(timing["resume_count"]) + 1


def build_result(task: dict[str, Any], state: dict[str, Any]) -> dict[str, Any]:
    executor = task.get("executor", {}) if isinstance(task.get("executor", {}), dict) else {}
    latest_checks = state.get("acceptance_runs", [])[-1] if state.get("acceptance_runs") else []
    total_checks = len(latest_checks) or (1 + len(require_string_list(task, "acceptance_commands")))
    accepted_checks = sum(1 for record in latest_checks if bool(record.get("passed")))
    quota = str(task.get("scarce_quota_burden", "medium"))
    if quota not in {"low", "medium", "high"}:
        quota = "medium"
    run_dir = Path(str(state.get("run_dir", "")))
    evidence = [f"task_contract={state.get('task_contract')}", f"state={run_dir / 'state.json'}"]
    if state.get("changed_paths"):
        evidence.append("changed_paths=" + ",".join(str(item) for item in state["changed_paths"]))
    return {
        "task_id": task["task_id"],
        "route": "native_cli",
        "acceptance": {"accepted": accepted_checks, "total": total_checks},
        "elapsed_min": round(float(ensure_timing(state)["active_elapsed_s"]) / 60.0, 3),
        "direct_cost_usd": None,
        "scarce_quota_burden": quota,
        "model_class": f"Codex CLI / configured:{executor.get('model', 'config-default')}",
        "attempts": max(1, int(state.get("executor_attempts", 0))),
        "evidence": evidence,
    }


def finalize_result(task: dict[str, Any], state: dict[str, Any], state_path: Path, started: float) -> None:
    timing = ensure_timing(state)
    timing["completed_invocation_elapsed_s"] = round(
        float(timing.get("completed_invocation_elapsed_s", 0.0)) + max(0.0, time.monotonic() - started), 3
    )
    if state.get("status") in FINAL_STATES and not timing.get("calendar_completed_at"):
        timing["calendar_completed_at"] = utc_now()
    state["result"] = build_result(task, state)
    save_state(state_path, state)


def initialize_run(task_path: Path, run_root: Path) -> tuple[dict[str, Any], dict[str, Any], Path]:
    task = read_json(task_path)
    validate_task(task)
    task_id = require_text(task, "task_id")
    if any(ch not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_." for ch in task_id):
        raise ContractError("task_id may contain only letters, digits, '-', '_' and '.'")
    run_dir = run_root / task_id
    state_path = run_dir / "state.json"
    if state_path.exists():
        raise ContractError(f"task state already exists; use --resume: {state_path}")
    run_dir.mkdir(parents=True, exist_ok=False)
    canonical_task = run_dir / "task.json"
    shutil.copy2(task_path, canonical_task)
    state = new_state(task, canonical_task, run_dir)
    save_state(state_path, state)
    return task, state, state_path


def load_run(task_path: Path, run_root: Path) -> tuple[dict[str, Any], dict[str, Any], Path]:
    requested = read_json(task_path)
    validate_task(requested)
    task_id = require_text(requested, "task_id")
    state_path = run_root / task_id / "state.json"
    if not state_path.is_file():
        raise ContractError(f"no saved state for task {task_id}")
    state = read_json(state_path)
    canonical = Path(require_text(state, "task_contract"))
    task = read_json(canonical)
    if task != requested:
        raise ContractError("resume task contract differs from the saved canonical contract")
    return task, state, state_path


def prepare_or_resume(
    task_path: Path,
    run_root: Path,
    resume: bool,
) -> tuple[dict[str, Any], dict[str, Any], Path, bool]:
    if not resume:
        task, state, state_path = initialize_run(task_path, run_root)
        ok_context, failures = check_context(task)
        ok_identity, identity_failures = check_repo_identity(task)
        if not ok_context or not ok_identity:
            state["failure"] = {"kind": "context_gate", "details": failures + identity_failures}
            save_state(state_path, state, "blocked_context")
            return task, state, state_path, False
        save_state(state_path, state, "preflight_passed")
        return task, state, state_path, True
    task, state, state_path = load_run(task_path, run_root)
    if state.get("status") in INFLIGHT_STATES:
        state["failure"] = {
            "kind": "uncertain_inflight",
            "message": "previous process stopped while executor state was in-flight; refusing automatic duplicate execution",
        }
        save_state(state_path, state, "uncertain_inflight")
        return task, state, state_path, False
    if state.get("status") in FINAL_STATES:
        return task, state, state_path, False
    record_resume_wait(state)
    ok_context, failures = check_context(task)
    ok_identity, identity_failures = check_repo_identity(task, check_clean=False)
    if not ok_context or not ok_identity:
        state["failure"] = {"kind": "resume_context_gate", "details": failures + identity_failures}
        save_state(state_path, state, "blocked_context")
        return task, state, state_path, False
    if not gate_changed_paths(task, state, state_path):
        return task, state, state_path, False
    return task, state, state_path, True


def run_task(task_path: Path, run_root: Path, resume: bool) -> tuple[int, dict[str, Any]]:
    started = time.monotonic()
    task, state, state_path, may_continue = prepare_or_resume(task_path, run_root, resume)
    if not may_continue:
        if state.get("status") in FINAL_STATES and state.get("result") is not None:
            return (0 if state["status"] == "accepted" else 2), state
        finalize_result(task, state, state_path, started)
        return (0 if state["status"] == "accepted" else 2), state

    max_repairs, executor_timeout, check_timeout = limits(task)
    if state["status"] == "preflight_passed":
        if not execute_one_turn(task, state, state_path, context_prompt(task), executor_timeout, repair=False):
            finalize_result(task, state, state_path, started)
            return 3, state
        if not gate_changed_paths(task, state, state_path):
            finalize_result(task, state, state_path, started)
            return 4, state

    checks = run_acceptance(task, check_timeout)
    state["acceptance_runs"].append(checks)
    record_acceptance_elapsed(state, checks)
    if not gate_changed_paths(task, state, state_path):
        finalize_result(task, state, state_path, started)
        return 4, state
    if acceptance_passed(checks):
        save_state(state_path, state, "accepted")
    else:
        save_state(state_path, state, "checks_failed")
    while state["status"] == "checks_failed" and state["repair_attempts"] < max_repairs:
        attempt = state["repair_attempts"] + 1
        prompt = repair_prompt(task, failed_checks(checks), attempt)
        if not execute_one_turn(task, state, state_path, prompt, executor_timeout, repair=True):
            finalize_result(task, state, state_path, started)
            return 3, state
        if not gate_changed_paths(task, state, state_path):
            finalize_result(task, state, state_path, started)
            return 4, state
        checks = run_acceptance(task, check_timeout)
        state["acceptance_runs"].append(checks)
        record_acceptance_elapsed(state, checks)
        if not gate_changed_paths(task, state, state_path):
            finalize_result(task, state, state_path, started)
            return 4, state
        if acceptance_passed(checks):
            save_state(state_path, state, "accepted")
        else:
            save_state(state_path, state, "checks_failed")

    if state["status"] == "checks_failed":
        state["failure"] = {
            "kind": "repair_limit_reached",
            "max_repair_attempts": max_repairs,
            "failed_checks": failed_checks(checks),
        }
        save_state(state_path, state, "repair_limit_reached")

    finalize_result(task, state, state_path, started)
    return (0 if state["status"] == "accepted" else 5), state


def main() -> int:
    if os.environ.get(LEAF_ENV) == "1":
        print(json.dumps({
            "status": "contract_error",
            "error": "recursive AirCoder CLI invocation is blocked in leaf executor mode",
        }, ensure_ascii=False, indent=2))
        return 2
    parser = argparse.ArgumentParser(description="Run one bounded AirCoder coding task through Codex CLI")
    parser.add_argument("--task", required=True, help="Path to the coding-task JSON contract")
    parser.add_argument("--run-root", help="Runtime state root; defaults to AIR_CODER_RUN_ROOT or E:/-4-/air-coder/runs")
    parser.add_argument("--resume", action="store_true", help="Resume a saved non-inflight task")
    args = parser.parse_args()
    try:
        code, state = run_task(Path(args.task), run_root_from_args(args.run_root), args.resume)
    except (ContractError, RuntimeError, OSError) as exc:
        print(json.dumps({"status": "contract_error", "error": str(exc)}, ensure_ascii=False, indent=2))
        return 2
    summary = {
        "task_id": state.get("task_id"),
        "status": state.get("status"),
        "thread_id": state.get("thread_id"),
        "executor_attempts": state.get("executor_attempts"),
        "repair_attempts": state.get("repair_attempts"),
        "changed_paths": state.get("changed_paths"),
        "failure": state.get("failure"),
        "result": state.get("result"),
        "state_file": str(run_root_from_args(args.run_root) / str(state.get("task_id")) / "state.json"),
    }
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
