"""Bounded, shell-free transport to the AirWorker binary."""
from __future__ import annotations

import hashlib
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
_TIMEOUTS = {"status": 30, "run": 1800, "verify": 300, "plan_new": 300}
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


def _check_completed_nodes(status: Optional[dict[str, Any]], binary: str, product: Path,
                           runs: list[dict[str, Any]], config: Optional[Path]) -> Optional[dict[str, Any]]:
    """Do not claim completion when the native node view is open or unreadable."""
    if not status:
        return status
    # Products without native plan nodes keep their existing status contract.
    # A referenced but missing native node still needs reconciliation.
    try:
        root_spine = product / "PLAN.md"
        if root_spine.exists():
            initial_spine = root_spine.read_text(encoding="utf-8")
            refs_present = bool(re.search(r"\[\[N-[0-9]{3}_", initial_spine))
        else:
            refs_present = False
        node_files = any((product / "plan").glob("N-*.md"))
        settings_path = config or product / "run-config.json"
        if settings_path.is_file():
            settings = json.loads(settings_path.read_text(encoding="utf-8"))
            configured = Path(settings.get("plan") or "PLAN.md")
            configured = (configured if configured.is_absolute() else product / configured).resolve()
            if not _is_within(configured, product):
                raise ValueError("configured plan outside product")
            if configured.is_file():
                refs_present = refs_present or bool(re.search(r"\[\[N-[0-9]{3}_", configured.read_text(encoding="utf-8")))
        if not refs_present and not node_files:
            return status
    except (OSError, UnicodeError, ValueError, TypeError, AttributeError):
        invalid = dict(status)
        invalid.pop("progress", None)
        invalid.update(outcome="error", stop_reason="plan_node_status_unavailable", next_action="inspect_plan")
        return invalid
    run = _invoke([binary, "plan", "node", "list", "-product", str(product), "-json"], product, 30)
    runs.append(run)
    try:
        nodes = json.loads(run["stdout"])
        if run["exit_code"] != 0 or run.get("timed_out") or not isinstance(nodes, list):
            raise ValueError("node list unavailable")
        if any(not isinstance(node, dict) or not isinstance(node.get("status"), str)
               or not isinstance(node.get("id"), str) for node in nodes):
            raise ValueError("invalid node list")
        pending = [node.get("id") for node in nodes if node["status"] != "closed"] if status.get("outcome") == "completed" else []
        spine = (product / "PLAN.md").read_text(encoding="utf-8")
        settings = json.loads((config or product / "run-config.json").read_text(encoding="utf-8"))
        configured_plan = Path(settings.get("plan") or "PLAN.md")
        configured_plan = (configured_plan if configured_plan.is_absolute() else product / configured_plan).resolve()
        if not _is_within(configured_plan, product):
            raise ValueError("configured plan outside product")
        if configured_plan != (product / "PLAN.md").resolve():
            configured = configured_plan.read_text(encoding="utf-8")
            if nodes or re.search(r"\[\[N-[0-9]{3}_", spine + configured):
                pending.append("different_plan_sources")
        by_id = {node["id"]: node["status"] for node in nodes}
        if len(by_id) != len(nodes):
            pending.append("duplicate_node")
        numbers = [m.group(1) for node_id in by_id if (m := re.match(r"^N-([0-9]{3})_", node_id))]
        if len(numbers) != len(nodes) or len(set(numbers)) != len(numbers):
            pending.append("duplicate_or_invalid_number")
        links = re.findall(r"(?m)^\s*-\s*\[(open|closed)\][^\n]*?\[\[(N-[0-9]{3}_[^\]|]+)(?:\|[^\]]*)?\]\]", spine)
        if len({item[1] for item in links}) != len(links):
            pending.append("duplicate_spine_node")
        all_refs = re.findall(r"\[\[(N-[0-9]{3}_[^\]|]+)(?:\|[^\]]*)?\]\]", spine)
        if any(node_id not in by_id for node_id in all_refs):
            pending.append("missing_node")
        for state, node_id in links:
            if by_id.get(node_id) != state or (state != "closed" and status.get("outcome") == "completed"):
                pending.append(node_id)
        if any(node_id not in {item[1] for item in links} for node_id in by_id):
            pending.append("unlinked_node")
    except (OSError, UnicodeError, ValueError, TypeError, KeyError, AttributeError):
        status = dict(status)
        status.pop("progress", None)
        status.update(outcome="error", stop_reason="plan_node_status_unavailable", next_action="inspect_plan")
        return status
    if pending:
        status = dict(status)
        status.pop("progress", None)
        status.update(outcome="error", stop_reason="plan_sources_disagree", next_action="inspect_plan")
    return status


def _readback_created(product: Path, binary: str, node_id: str, values: Mapping[str, str]) -> bool:
    if not re.fullmatch(r"N-[0-9]{3}_[\w-]+", node_id):
        return False
    run = _invoke([binary, "plan", "node", "list", "-product", str(product), "-json"], product, 30)
    try:
        nodes = json.loads(run["stdout"])
        matches = [n for n in nodes if isinstance(n, dict) and n.get("id") == node_id]
        spine = (product / "PLAN.md").read_text(encoding="utf-8")
        return bool(run["exit_code"] == 0 and not run.get("timed_out") and len(matches) == 1
                    and all(matches[0].get(k) == values[k] for k in ("title", "parent", "owner", "done_when"))
                    and (product / "plan" / (node_id + ".md")).is_file() and "[[" + node_id + "]]" in spine)
    except (OSError, UnicodeError, ValueError, TypeError, KeyError):
        return False


def _plan_new(args: Mapping[str, Any], product: Path, binary: str, session_id: str) -> str:
    """Use the prototype's native mutation with a durable fail-closed request record."""
    from . import hooks
    fields = ("title", "parent", "owner", "done_when", "request_id")
    if any(not isinstance(args.get(k), str) or not args[k].strip() for k in fields):
        return _envelope("plan_new", stop_reason="plan_fields_required")
    request_id = args["request_id"].strip()
    if len(request_id.encode("utf-8")) > 256 or _redact_text(request_id) != request_id:
        return _envelope("plan_new", stop_reason="request_id_invalid")
    values = {k: args[k].strip() for k in fields}
    fingerprint = hashlib.sha256(json.dumps(values, ensure_ascii=False, sort_keys=True).encode()).hexdigest()
    key = hashlib.sha256((os.path.normcase(str(product)) + "\0" + request_id).encode()).hexdigest()
    try:
        requests = product / ".air-worker" / "hermes-plan-requests"
        if not _is_within(requests.resolve(), product):
            raise OSError("request store outside product")
        requests.mkdir(mode=0o700, parents=True, exist_ok=True)
        record = requests / (key + ".json")
        fd = os.open(str(record), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    except FileExistsError:
        try:
            previous = json.loads(record.read_text(encoding="utf-8"))
            if previous.get("fingerprint") != fingerprint:
                return _envelope("plan_new", product=str(product), stop_reason="request_id_conflict")
            if previous.get("state") != "created":
                return _envelope("plan_new", product=str(product), outcome="unknown", stop_reason="request_pending_do_not_retry")
            node_id = previous["node_id"]
            if not isinstance(node_id, str) or not _readback_created(product, binary, node_id, values):
                raise ValueError("request readback failed")
            return _envelope("plan_new", product=str(product), outcome="created", exit_code=0, node_id=node_id, replayed=True)
        except (OSError, ValueError, TypeError, KeyError):
            return _envelope("plan_new", product=str(product), outcome="unknown", stop_reason="request_record_unverified_do_not_retry")
    except OSError:
        return _envelope("plan_new", product=str(product), stop_reason="request_record_unavailable")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump({"fingerprint": fingerprint, "state": "pending"}, stream)
            stream.flush()
            os.fsync(stream.fileno())
    except OSError:
        return _envelope("plan_new", product=str(product), outcome="unknown", stop_reason="request_record_write_failed_do_not_retry")
    lock_path = requests / "product-mutation.lock"
    try:
        lock_fd = os.open(str(lock_path), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(lock_fd, "w", encoding="utf-8") as stream:
            stream.write(key)
            stream.flush()
            os.fsync(stream.fileno())
    except OSError:
        return _envelope("plan_new", product=str(product), outcome="unknown", stop_reason="product_mutation_locked_do_not_retry")
    if not hooks.invalidate_product(str(product)):
        # No native mutation was launched, so our own lock is safe to release.
        try:
            if lock_path.read_text(encoding="utf-8") == key:
                lock_path.unlink()
        except OSError:
            _LOG.error("plan mutation lock could not be released")
        return _envelope("plan_new", product=str(product), outcome="unknown", stop_reason="product_pending_do_not_retry")
    release_hold = False
    try:
        spine = (product / "PLAN.md").read_text(encoding="utf-8")
        parent = values["parent"]
        headings = {m.group(1).strip().rstrip("#").strip() for m in re.finditer(r"(?m)^#{1,6}\s+(.+?)\s*$", spine)}
        if parent not in headings and "[[" + parent + "]]" not in spine:
            release_hold = True
            return _envelope("plan_new", product=str(product), stop_reason="parent_not_found")
        listing = _invoke([binary, "plan", "node", "list", "-product", str(product), "-json"], product, 30)
        nodes = json.loads(listing["stdout"])
        if listing["exit_code"] or listing.get("timed_out") or not isinstance(nodes, list):
            raise ValueError("native node list unavailable")
        ids = [n["id"] for n in nodes]
        if any(not isinstance(n, str) or not re.fullmatch(r"N-[0-9]{3}_[\w-]+", n) for n in ids):
            raise ValueError("invalid node identifier")
        numbers = [n[2:5] for n in ids]
        refs = re.findall(r"\[\[(N-[0-9]{3}_[^\]|]+)(?:\|[^\]]*)?\]\]", spine)
        if len(numbers) != len(set(numbers)) or any(ref not in ids for ref in refs):
            release_hold = True
            return _envelope("plan_new", product=str(product), stop_reason="plan_sources_disagree")
        argv = [binary, "plan", "node", "new", "-product", str(product), "-title", values["title"],
                "-parent", parent, "-owner", values["owner"], "-done-when", values["done_when"],
                "-request-id", request_id, "-actor", session_id, "-actor-kind", "gpt-window", "-json"]
        run = _invoke(argv, product, _TIMEOUTS["plan_new"])
        native = json.loads(run["stdout"])
        node = native["node"]
        node_id = node["id"]
        if (run["exit_code"] or run.get("timed_out") or native.get("schema") != "air-worker.plan.node.mutation/v1"
                or native.get("action") != "created" or not re.fullmatch(r"N-[0-9]{3}_[\w-]+", node_id)
                or any(node.get(k) != values[k] for k in ("title", "parent", "owner", "done_when"))):
            raise ValueError("unconfirmed native mutation")
        if not _readback_created(product, binary, node_id, values):
            raise ValueError("node readback failed")
        temp = record.with_name(record.name + "." + uuid.uuid4().hex + ".tmp")
        with temp.open("x", encoding="utf-8") as stream:
            json.dump({"fingerprint": fingerprint, "state": "created", "node_id": node_id}, stream)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temp, record)
        release_hold = True
        return _envelope("plan_new", product=str(product), outcome="created", exit_code=0, node_id=node_id)
    except (OSError, UnicodeError, ValueError, TypeError, KeyError, AttributeError):
        return _envelope("plan_new", product=str(product), outcome="unknown", stop_reason="plan_new_unconfirmed_do_not_retry")
    finally:
        if release_hold:
            hooks.finish_product(str(product))
            try:
                if lock_path.read_text(encoding="utf-8") == key:
                    lock_path.unlink()
            except OSError:
                _LOG.error("plan mutation lock could not be released")


def execute(args: Mapping[str, Any], **kwargs: Any) -> str:
    action = str(args.get("action") or "").strip().lower()
    raw = str(args.get("product") or "").strip()
    if action not in {"status", "run", "verify"} or not raw:
        return _execute(args, **kwargs)
    try:
        from . import hooks
        product = str(Path(raw).expanduser().resolve())
        if not hooks.begin_operation(product):
            return _envelope(action, product=product, stop_reason="product_pending")
    except (OSError, RuntimeError, ValueError, ImportError):
        return _envelope(action, stop_reason="hooks_unavailable")
    try:
        return _execute(args, **kwargs)
    finally:
        hooks.end_operation(product)


def _execute(args: Mapping[str, Any], **kwargs: Any) -> str:
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
        if action == "plan_new":
            return _envelope(action, stop_reason="hooks_unavailable")
        enforcing = False
    if (enforcing or action == "plan_new") and not session_id:
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

    if action == "plan_new":
        return _plan_new(args, product, binary, session_id)

    from . import hooks
    status_generation = hooks.generation(str(product))

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
        status = _check_completed_nodes(status, binary, product, runs, config)
        operation = status_run
        if enforcing and _orphaned(status):
            promoted = True
            operation = _invoke(command("loop", identity=True), product, _TIMEOUTS["run"])
            runs.append(operation)
            status_run = _invoke(command("adapter", "-action", "status", identity=True), product, _TIMEOUTS["status"])
            runs.append(status_run)
            status = _parse(status_run)
            status = _check_completed_nodes(status, binary, product, runs, config)
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
        status = _check_completed_nodes(status, binary, product, runs, config)
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
        status = _check_completed_nodes(status, binary, product, runs, config)
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
    if status and status.get("stop_reason") in {"plan_sources_disagree", "plan_node_status_unavailable"}:
        result["exit_code"] = 2
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
        hooks.record_status(str(product), result, session_id=session_id, generation=status_generation)
    except Exception:
        pass
    return json.dumps(result, ensure_ascii=False, separators=(",", ":"))
