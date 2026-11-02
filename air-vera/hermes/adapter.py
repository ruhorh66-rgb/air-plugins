from __future__ import annotations

import json
import os
import subprocess
from pathlib import Path

_ALLOWED = {"status", "list", "package-health", "task-evidence"}


def _accepted_release_root(install_root: Path) -> Path:
    # Preserve Android's /data/user path. Resolving its /data/data alias changes
    # the SELinux execution decision even though both names reference one inode.
    root = Path(os.path.abspath(os.fspath(install_root)))
    pointer = root / "accepted.json"
    if not pointer.is_file():
        return root
    try:
        data = json.loads(pointer.read_text(encoding="utf-8"))
        version = data["version"]
        runtime_root = data["runtime_root"]
    except (OSError, KeyError, TypeError, json.JSONDecodeError) as exc:
        raise RuntimeError("AirVera accepted release pointer is invalid") from exc
    if data.get("product") != "air-vera" or not isinstance(version, str) or not version:
        raise RuntimeError("AirVera accepted release pointer is invalid")
    if not isinstance(runtime_root, str) or not runtime_root:
        raise RuntimeError("AirVera accepted release pointer is invalid")
    expected = Path(os.path.abspath(os.fspath(root / "versions" / version)))
    candidate = Path(os.path.abspath(runtime_root))
    if os.path.normcase(os.fspath(candidate)) != os.path.normcase(os.fspath(expected)) or not candidate.is_dir():
        raise RuntimeError("AirVera accepted release pointer is invalid")
    return candidate


def _execute_action(action: str) -> dict:
    if action not in _ALLOWED:
        raise ValueError("unsupported AirVera action")
    home = Path(os.environ.get("HERMES_HOME", Path.home() / ".hermes"))
    install_root = Path(os.environ.get("AIR_VERA_HOME", home / "air-vera"))
    try:
        release_root = _accepted_release_root(install_root)
    except RuntimeError as exc:
        return {"status": "ERROR", "error": str(exc)}
    binary = release_root / "bin" / "air-vera"
    runtime = install_root / "runtime"
    if not binary.is_file():
        return {"status": "ERROR", "error": "AirVera binary is not installed", "path": str(binary)}
    env = {
        **os.environ,
        "AIR_VERA_AUTOMATION_RUNTIME": str(runtime),
        "AIR_VERA_BUNDLE_ROOT": str(release_root),
    }
    if action != "package-health":
        env["AIR_VERA_PRODUCT_ROOT"] = str(release_root)
    else:
        env.pop("AIR_VERA_PRODUCT_ROOT", None)
    result = subprocess.run(
        [str(binary), action, "--json"],
        cwd=release_root,
        env=env,
        text=True,
        capture_output=True,
        timeout=30,
        check=False,
    )
    try:
        response = json.loads(result.stdout)
    except json.JSONDecodeError:
        if result.returncode:
            return {"status": "ERROR", "exit_code": result.returncode, "stderr": result.stderr[-2000:]}
        return {"status": "ERROR", "error": "control plane returned invalid JSON"}
    if result.returncode and isinstance(response, dict):
        response.setdefault("exit_code", result.returncode)
    return response


def execute(params=None, **kwargs) -> str:
    """Hermes tool handler for the installed AirVera control plane.

    Hermes v0.21 dispatches plugin handlers as ``handler(params, **kwargs)``,
    where ``params`` is the model argument dict and ``kwargs`` may contain
    runtime metadata such as ``task_id``.  Older AirVera releases accepted the
    action string directly; keep that form so local smoke probes remain usable.
    Plugin handlers return JSON text for the Hermes registry contract.
    """
    del kwargs
    if isinstance(params, dict):
        if set(params) - {"action", "task_id"}:
            return json.dumps({"status": "NOT_AVAILABLE", "reason": "UNEXPECTED_ARGUMENT"}, ensure_ascii=False)
        action = params.get("action")
        task_id = params.get("task_id")
    else:
        action, task_id = params, None
    if not isinstance(action, str) or action not in _ALLOWED:
        return json.dumps({"status": "NOT_AVAILABLE", "reason": "UNSUPPORTED_ACTION"}, ensure_ascii=False)
    if action == "task-evidence":
        import re
        if not isinstance(task_id, str) or re.fullmatch(r"TASK-OBS-[0-9]{4}", task_id) is None:
            return json.dumps({"status": "NOT_AVAILABLE", "reason": "INVALID_TASK_ID"}, ensure_ascii=False)
    elif task_id is not None:
        return json.dumps({"status": "NOT_AVAILABLE", "reason": "TASK_ID_ONLY_FOR_EVIDENCE"}, ensure_ascii=False)
    if os.name == "nt":
        from .windows_adapter import execute_action
        result = execute_action(action, task_id)
    elif action == "task-evidence":
        result = {"status": "NOT_AVAILABLE", "reason": "FEATURE_NOT_INSTALLED"}
    else:
        result = _execute_action(action)
    return json.dumps(result, ensure_ascii=False)
