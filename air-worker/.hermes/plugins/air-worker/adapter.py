"""Bounded, shell-free transport to the AirWorker binary."""
from __future__ import annotations

import json
import logging
import os
import re
import shutil
import subprocess
import time
import uuid
from pathlib import Path
from typing import Any, Mapping, Optional

SCHEMA = "air-worker.tool/v1"
_TIMEOUTS = {"status": 30, "run": 1800, "verify": 300}
_FIELDS = ("outcome", "progress", "current_step", "next_action", "stop_reason", "receipts", "receipts_omitted", "workers", "detail_path")
_RESULT_LIMIT = 8
_PRINCIPAL = "hermes"
_SECRET_KEY = re.compile(r"(?i)(?:secret|token|password|passwd|pwd|authorization|auth|cookie|api[_-]?key|private[_-]?key|credential)")
_KEY_VALUE = re.compile(
    r"(?i)(\b(?:access[_-]?token|refresh[_-]?token|token|password|passwd|pwd|authorization|auth|cookie|api[_-]?key|private[_-]?key|credential)\b[\"']?\s*[:=]\s*[\"']?)([^\s\"',;&}]+)"
)
_BEARER = re.compile(r"(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]+")
_TOKEN_SHAPE = re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{16,}|sk-[A-Za-z0-9_-]{16,}|[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})\b")
_LOG_LIMIT = 8192
_LOG = logging.getLogger(__name__)


def _is_within(path: Path, root: Path) -> bool:
    try:
        path.relative_to(root)
        return True
    except ValueError:
        return False


def _binary(product: Path) -> str:
    configured = os.environ.get("AIR_WORKER_BIN", "").strip()
    if configured:
        path = Path(configured).expanduser()
        if not path.is_absolute() or not path.is_file():
            raise FileNotFoundError("AIR_WORKER_BIN must name an absolute file")
        return str(path.resolve())
    found = shutil.which("air-worker")
    if not found:
        raise FileNotFoundError("air-worker binary not found; set AIR_WORKER_BIN")
    path = Path(found).resolve()
    if not path.is_file() or _is_within(path, product):
        raise FileNotFoundError("installed air-worker binary is not trusted")
    return str(path)


def _config(raw: Any, product: Path) -> Optional[Path]:
    value = str(raw or "").strip()
    if not value:
        return None
    requested = Path(value).expanduser()
    path = (requested if requested.is_absolute() else product / requested).resolve()
    if not _is_within(path, product):
        raise ValueError("config must resolve under product")
    if not path.is_file():
        raise FileNotFoundError("config file not found")
    return path


def _log_dir() -> Path:
    configured = os.environ.get("AIR_WORKER_LOG_DIR", "").strip()
    path = Path(configured).expanduser() if configured else Path(os.environ.get("HERMES_HOME", str(Path.home() / ".hermes"))) / "plugin-data" / "air-worker" / "logs"
    path.mkdir(parents=True, exist_ok=True)
    try:
        os.chmod(path, 0o700)
    except OSError:
        pass
    return path


def _text(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", errors="replace")
    return str(value)


def _redact_text(value: Any) -> str:
    text = _text(value)
    for key, secret in os.environ.items():
        if _SECRET_KEY.search(key) and len(secret) >= 4:
            text = text.replace(secret, "[REDACTED]")
    text = _BEARER.sub(r"\1[REDACTED]", text)
    text = _KEY_VALUE.sub(r"\1[REDACTED]", text)
    return _TOKEN_SHAPE.sub("[REDACTED]", text)


def _redact(value: Any) -> Any:
    if isinstance(value, str) or isinstance(value, bytes):
        return _redact_text(value)
    if isinstance(value, list):
        return [_redact(item) for item in value]
    if isinstance(value, dict):
        return {str(key): ("[REDACTED]" if _SECRET_KEY.search(str(key)) else _redact(item)) for key, item in value.items()}
    return value


def _invoke(argv: list[str], product: Path, timeout: int) -> dict[str, Any]:
    started = time.monotonic()
    try:
        done = subprocess.run(
            argv,
            cwd=str(product),
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout,
            shell=False,
            check=False,
        )
        return {"argv": argv, "exit_code": done.returncode, "duration_ms": int((time.monotonic() - started) * 1000), "stdout": _text(done.stdout), "stderr": _text(done.stderr)}
    except subprocess.TimeoutExpired as exc:
        return {"argv": argv, "exit_code": 124, "duration_ms": int((time.monotonic() - started) * 1000), "stdout": _text(exc.stdout), "stderr": _text(exc.stderr), "timed_out": True}
    except OSError:
        _LOG.error("air-worker process launch failed")
        return {"argv": argv, "exit_code": 126, "duration_ms": int((time.monotonic() - started) * 1000), "stdout": "", "stderr": "process launch failed", "launch_error": True}


def _parse(run: Mapping[str, Any]) -> Optional[dict[str, Any]]:
    text = _text(run.get("stdout")).strip()
    for candidate in [text] + [line.strip() for line in reversed(text.splitlines()) if line.strip().startswith("{")]:
        try:
            value = json.loads(candidate)
        except (TypeError, ValueError):
            continue
        if isinstance(value, dict) and value.get("schema_version") == SCHEMA:
            return value
    return None


def _save(action: str, runs: list[dict[str, Any]]) -> Optional[Path]:
    try:
        name = f"{time.strftime('%Y%m%dT%H%M%SZ', time.gmtime())}-{action}-{uuid.uuid4().hex[:8]}.json"
        path = _log_dir() / name
        compact_runs = []
        for run in runs:
            compact_runs.append(
                {
                    "argv": _redact(run.get("argv", [])),
                    "exit_code": run.get("exit_code"),
                    "duration_ms": run.get("duration_ms"),
                    "timed_out": bool(run.get("timed_out", False)),
                    "launch_error": bool(run.get("launch_error", False)),
                    "stdout": _redact_text(run.get("stdout"))[:_LOG_LIMIT],
                    "stderr": _redact_text(run.get("stderr"))[:_LOG_LIMIT],
                }
            )
        payload = json.dumps({"schema": SCHEMA, "action": action, "runs": compact_runs}, ensure_ascii=False, separators=(",", ":"))
        fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(payload)
        try:
            os.chmod(path, 0o600)
        except OSError:
            pass
        return path
    except OSError:
        _LOG.error("air-worker transport log write failed")
        return None


def _envelope(action: str, **values: Any) -> str:
    result = {"schema_version": SCHEMA, "action": action, "outcome": "error", "exit_code": 2}
    result.update(values)
    return json.dumps(_redact(result), ensure_ascii=False, separators=(",", ":"))


def _orphaned(status: Optional[Mapping[str, Any]]) -> bool:
    if not isinstance(status, Mapping):
        return False
    progress = status.get("progress")
    if not isinstance(progress, Mapping):
        return False
    closed, total = progress.get("closed"), progress.get("total")
    return bool(
        isinstance(closed, int)
        and not isinstance(closed, bool)
        and isinstance(total, int)
        and not isinstance(total, bool)
        and closed < total
        and status.get("next_action") == "run_loop"
        and status.get("stop_reason") == "no_live_worker"
        and not status.get("workers")
    )


def execute(args: Mapping[str, Any], **kwargs: Any) -> str:
    action = str(args.get("action") or "").strip().lower()
    if action not in _TIMEOUTS:
        return _envelope(action, stop_reason="invalid_action")
    raw = str(args.get("product") or "").strip()
    if not raw:
        return _envelope(action, stop_reason="product_required")
    session_id = str(kwargs.get("session_id") or "").strip()
    try:
        from . import hooks
        enforcing = hooks.is_enforcing()
    except Exception:
        enforcing = False
    if enforcing and not session_id:
        return _envelope(action, stop_reason="session_id_required")
    try:
        product = Path(raw).expanduser().resolve()
        if not product.is_dir():
            return _envelope(action, stop_reason="product_not_found")
        binary = _binary(product)
    except OSError:
        return _envelope(action, stop_reason="binary_not_found")
    try:
        config = _config(args.get("config"), product)
    except (OSError, ValueError):
        return _envelope(action, stop_reason="config_invalid")

    def command(*parts: str, identity: bool = False) -> list[str]:
        argv = [binary, *parts, "-product", str(product)]
        if config is not None:
            argv.extend(["-config", str(config)])
        if identity and session_id:
            argv.extend(["-principal", _PRINCIPAL, "-session-key", session_id])
        return argv

    runs: list[dict[str, Any]] = []
    promoted = False
    if action == "status":
        status_run = _invoke(command("adapter", "-action", "status", identity=True), product, _TIMEOUTS["status"])
        runs.append(status_run)
        status = _parse(status_run)
        operation = status_run
        if enforcing and _orphaned(status):
            promoted = True
            operation = _invoke(command("loop", identity=True), product, _TIMEOUTS["run"])
            runs.append(operation)
            status_run = _invoke(command("adapter", "-action", "status", identity=True), product, _TIMEOUTS["status"])
            runs.append(status_run)
            status = _parse(status_run)
    else:
        operation = _invoke(
            command("loop", identity=True) if action == "run" else command("judge"),
            product,
            _TIMEOUTS[action],
        )
        runs.append(operation)
        status_run = _invoke(command("adapter", "-action", "status", identity=True), product, _TIMEOUTS["status"])
        runs.append(status_run)
        status = _parse(status_run)
    verification_run: dict[str, Any] | None = None
    auto_verified = False
    if (
        enforcing
        and action in {"status", "run"}
        and operation["exit_code"] == 0
        and status_run["exit_code"] == 0
        and status is not None
        and status.get("outcome") == "completed"
    ):
        # Hermes 0.21.3 cannot force a second model turn from a terminal-output
        # hook.  Close the continuity transaction inside the native tool instead:
        # a completed enforce status/run is judged and re-read before it returns.
        verification_run = _invoke(command("judge"), product, _TIMEOUTS["verify"])
        runs.append(verification_run)
        status_run = _invoke(command("adapter", "-action", "status", identity=True), product, _TIMEOUTS["status"])
        runs.append(status_run)
        status = _parse(status_run)
        auto_verified = bool(
            verification_run["exit_code"] == 0
            and status_run["exit_code"] == 0
            and status is not None
            and status.get("outcome") == "completed"
        )
    success = bool(
        operation["exit_code"] == 0
        and status_run["exit_code"] == 0
        and status is not None
        and (verification_run is None or verification_run["exit_code"] == 0)
    )
    continuity_violation = bool(
        (action == "run" or promoted)
        and success
        and _orphaned(status)
    )
    result: dict[str, Any] = {
        "schema_version": SCHEMA,
        "action": action,
        "exit_code": (
            operation["exit_code"]
            or (verification_run["exit_code"] if verification_run is not None else 0)
            or status_run["exit_code"]
        ),
        "product": str(product),
    }
    if auto_verified:
        result["verified"] = True
        result["verification_action"] = "verify"
    log_path = _save(action, runs)
    if log_path is not None:
        result["log_path"] = str(log_path)
    if status:
        result.update({key: status[key] for key in _FIELDS if key in status})
        for key in ("receipts", "workers"):
            if isinstance(result.get(key), list):
                result[key] = result[key][:_RESULT_LIMIT]
    else:
        result["stop_reason"] = "adapter_status_invalid"
    result["outcome"] = status.get("outcome", "error") if success and not continuity_violation else "error"
    if operation.get("timed_out"):
        result["stop_reason"] = f"{'run' if promoted else action}_timeout"
    elif verification_run is not None and verification_run.get("timed_out"):
        result["stop_reason"] = "verify_timeout"
    elif operation.get("launch_error") or status_run.get("launch_error") or (verification_run is not None and verification_run.get("launch_error")):
        result["stop_reason"] = "launch_failed"
    elif operation["exit_code"] and (action in {"run", "verify"} or promoted):
        result["stop_reason"] = f"{'run' if promoted else action}_exit_{operation['exit_code']}"
    elif verification_run is not None and verification_run["exit_code"]:
        result["stop_reason"] = f"verify_exit_{verification_run['exit_code']}"
    elif continuity_violation:
        result["exit_code"] = 2
        result["stop_reason"] = "continuity_violation"
    result = _redact(result)
    try:
        from . import hooks
        hooks.record_status(str(product), result, session_id=session_id)
    except Exception:
        pass
    return json.dumps(result, ensure_ascii=False, separators=(",", ":"))
