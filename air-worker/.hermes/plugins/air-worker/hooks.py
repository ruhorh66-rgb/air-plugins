"""Deterministic lifecycle and policy hooks; no network, LLM, or workflow state machine.

The in-memory records below are only same-process enforcement evidence.  They do
not claim that the AirWorker binary owns, or can authenticate, a Hermes session;
the current adapter API does not carry such an ownership contract.  A plugin
restart therefore discards verification evidence and completion fails closed.
"""
from __future__ import annotations

import json
import os
import re
import threading
from pathlib import Path
from typing import Any, Mapping, Optional

_LOCK = threading.RLock()
_BINDINGS: set[tuple[str, str]] = set()
_STATUS: dict[tuple[str, str], dict[str, Any]] = {}
_EVENTS: list[dict[str, Any]] = []
_MAX = 128
_ENFORCE_PROFILE = "airworker-hermes-v1-enforce"
_ENFORCING = False

# Explicitly safe observer tools.  Namespaces are removed before classification.
_READ_ONLY = frozenset({
    "read_file", "search_files", "web_search", "web_extract", "vision_analyze",
    "skill_view", "skills_list", "tool_search", "tool_describe", "session_search",
})
# These tools are safely scoped by an explicit path only in the listed forms.
_PATH_MUTATORS = frozenset({"write_file"})
_PROCESS_OR_MUTATING = frozenset({
    "terminal", "execute_code", "patch", "process_manage", "delegate_task",
    "skill_manage", "text_to_speech", "tool_call",
})
_PATH_KEYS = ("path", "file_path", "target_path", "output_path")


def configure(profile_name: Any) -> None:
    """Configure policy from Hermes' public profile identity."""
    global _ENFORCING
    with _LOCK:
        _ENFORCING = str(profile_name or "").strip() == _ENFORCE_PROFILE


def is_enforcing() -> bool:
    with _LOCK:
        return _ENFORCING


def _sid(k: Mapping[str, Any]) -> str:
    """Return only Hermes' canonical session id, never a task/call id."""
    return str(k.get("session_id") or "").strip()


def _canonical(path: Any, base: Optional[Path] = None) -> Optional[Path]:
    if path in (None, ""):
        return None
    try:
        value = Path(str(path)).expanduser()
        if not value.is_absolute() and base is not None:
            value = base / value
        return value.resolve(strict=False)
    except (OSError, RuntimeError, TypeError, ValueError):
        return None


def _managed_root(value: Any, base: Optional[Path] = None) -> Optional[str]:
    path = _canonical(value, base)
    if path is None:
        return None
    start = path if path.is_dir() else path.parent
    for candidate in (start, *start.parents):
        try:
            if (candidate / "run-config.json").is_file() or (candidate / "PLAN.md").is_file():
                return str(candidate)
        except OSError:
            continue
    return None


def _argument_map(k: Mapping[str, Any]) -> Mapping[str, Any]:
    args = k.get("args")
    return args if isinstance(args, Mapping) else {}


def _product(k: Mapping[str, Any]) -> Optional[str]:
    args = _argument_map(k)
    base = _canonical(args.get("workdir") or args.get("cwd") or k.get("cwd") or k.get("workdir") or os.getcwd())
    candidates: list[Any] = [
        k.get("product"), k.get("project_path"), k.get("cwd"), k.get("workdir"),
        args.get("product"), args.get("project_path"), args.get("cwd"), args.get("workdir"),
        os.environ.get("AIR_WORKER_PRODUCT"),
    ]
    changed = k.get("changed_paths")
    if isinstance(changed, (list, tuple)):
        candidates.extend(changed)
    for key in _PATH_KEYS:
        candidates.append(args.get(key))
    for value in candidates:
        if value not in (None, ""):
            product = _managed_root(value, base)
            if product:
                return product
    return None


def _remember_binding(sid: str, product: str) -> None:
    if sid and product:
        _BINDINGS.add((sid, product))


def bind_session(**kwargs: Any) -> None:
    sid, product = _sid(kwargs), _product(kwargs)
    if sid and product:
        with _LOCK:
            _remember_binding(sid, product)


def unbind_session(**kwargs: Any) -> None:
    # Hermes' canonical on_session_end payload is emitted at turn finalization,
    # not only when the canonical session ceases to exist.  Keep same-session
    # evidence across ordinary turns; reduced exit/reset payloads still clear it.
    if kwargs.get("turn_id") or any(key in kwargs for key in ("completed", "failed", "interrupted")):
        return
    sid = _sid(kwargs)
    if not sid:
        return
    with _LOCK:
        doomed = {key for key in _BINDINGS if key[0] == sid}
        _BINDINGS.difference_update(doomed)
        for key in list(_STATUS):
            if key[0] == sid:
                del _STATUS[key]


def record_status(product: str, status: Mapping[str, Any], session_id: str = "") -> None:
    """Cache the latest adapter result as enforcement evidence, not binary state."""
    resolved = _canonical(product)
    if resolved is None:
        return
    product_key = str(resolved)
    sid = str(session_id or "").strip()
    keys = (
        "schema_version", "action", "outcome", "exit_code", "progress",
        "current_step", "next_action", "stop_reason", "receipts", "evidence",
        "workers", "detail_path", "log_path",
    )
    with _LOCK:
        # Compatibility for direct callers: infer only when exactly one canonical
        # session is already bound to this product.  Never guess between sessions.
        if not sid:
            owners = {bound_sid for bound_sid, bound_product in _BINDINGS if bound_product == product_key}
            if len(owners) == 1:
                sid = next(iter(owners))
        if not sid:
            return
        _remember_binding(sid, product_key)
        _STATUS[(sid, product_key)] = {key: status[key] for key in keys if key in status}


def _bound(k: Mapping[str, Any]) -> tuple[Optional[str], Optional[dict[str, Any]]]:
    sid = _sid(k)
    discovered = _product(k)
    with _LOCK:
        if sid and discovered:
            _remember_binding(sid, discovered)
        product = discovered
        if sid and not product:
            products = {p for bound_sid, p in _BINDINGS if bound_sid == sid}
            if len(products) == 1:
                product = next(iter(products))
        status = dict(_STATUS.get((sid, product), {})) if sid and product else None
        return product, status


def pre_llm_call(**kwargs: Any):
    product, status = _bound(kwargs)
    if not product:
        return None
    if not status:
        return {"context": f"AirWorker managed product: {product}. Call air_worker status before acting."}
    fields = []
    progress = status.get("progress")
    if isinstance(progress, dict):
        fields.append(f"closed/total={progress.get('closed', 0)}/{progress.get('total', 0)}")
    fields.extend(
        f"{key}={status[key]}" for key in
        ("action", "outcome", "current_step", "next_action", "stop_reason", "detail_path", "log_path")
        if status.get(key) not in (None, "", [])
    )
    return {"context": f"AirWorker managed product: {product}; {'; '.join(fields) or 'status=unknown'}. Use only air_worker for transitions."}


def _tool_leaf(name: Any) -> str:
    parts = [part for part in re.split(r"(?:__|[.:/])", str(name or "").strip().lower()) if part]
    return parts[-1] if parts else ""


def _is_within(path: Path, product: Path) -> bool:
    try:
        path.relative_to(product)
        return True
    except ValueError:
        return False


def _explicit_targets(args: Mapping[str, Any], product: str) -> list[Path]:
    base = _canonical(args.get("workdir") or args.get("cwd")) or Path(product)
    targets: list[Path] = []
    for key in _PATH_KEYS:
        value = args.get(key)
        if value not in (None, ""):
            target = _canonical(value, base)
            if target is not None:
                targets.append(target)
    return targets


def pre_tool_call(**kwargs: Any):
    tool = _tool_leaf(kwargs.get("tool_name"))
    if tool == "air_worker":
        return None
    product, _ = _bound(kwargs)
    if not product:
        return None
    if not is_enforcing():
        return None
    if tool in _READ_ONLY:
        return None
    args = _argument_map(kwargs)
    if tool in _PATH_MUTATORS:
        targets = _explicit_targets(args, product)
        product_path = Path(product)
        # An explicitly and completely out-of-product write is not an AirWorker
        # bypass.  Missing/ambiguous paths remain fail-closed.
        if targets and all(not _is_within(target, product_path) for target in targets):
            return None
    # Known process/mutating tools and unclassified tools both fail closed for a
    # managed session.  Only the explicit read-only set is confidently safe.
    kind = "process or mutating" if tool in _PROCESS_OR_MUTATING else "unclassified"
    return {
        "action": "block",
        "message": f"Managed AirWorker product {product}: {kind} tool '{tool or 'unknown'}' is blocked; use air_worker run or verify.",
    }


def _parsed_result(result: Any) -> Optional[dict[str, Any]]:
    if isinstance(result, Mapping):
        return dict(result)
    if isinstance(result, str) and result.lstrip().startswith("{"):
        try:
            parsed = json.loads(result)
            return parsed if isinstance(parsed, dict) else None
        except ValueError:
            return None
    return None


def post_tool_call(**kwargs: Any) -> None:
    product, _ = _bound(kwargs)
    if not product:
        return
    parsed = _parsed_result(kwargs.get("result"))
    tool = _tool_leaf(kwargs.get("tool_name"))
    if tool == "air_worker":
        if parsed is not None:
            record_status(str(parsed.get("product") or product), parsed, session_id=_sid(kwargs))
        else:
            args = _argument_map(kwargs)
            record_status(product, {
                "action": str(args.get("action") or "unknown"),
                "outcome": "error", "exit_code": None, "stop_reason": "tool_result_invalid",
            }, session_id=_sid(kwargs))
    meta = {}
    if parsed is not None:
        meta = {key: parsed[key] for key in ("schema_version", "action", "outcome", "exit_code", "detail_path", "log_path") if key in parsed}
    event = {
        "event": "tool", "tool": str(kwargs.get("tool_name") or ""),
        "task_id": str(kwargs.get("task_id") or ""), "duration_ms": kwargs.get("duration_ms"),
        "result": meta,
    }
    with _LOCK:
        _EVENTS.append(event)
        del _EVENTS[:-_MAX]


def pre_verify(**kwargs: Any):
    product, status = _bound(kwargs)
    if not product:
        return None
    if not is_enforcing():
        return None
    sid = _sid(kwargs)
    if not sid:
        return {"action": "continue", "message": "AirWorker completion has no canonical Hermes session id; run air_worker verify in this session."}
    if not status:
        return {"action": "continue", "message": "AirWorker verification evidence is missing for this session; run air_worker verify."}
    if status.get("action") != "verify":
        return {"action": "continue", "message": "The latest same-session AirWorker action was not verify; run air_worker verify."}
    exit_code = status.get("exit_code")
    if isinstance(exit_code, bool) or not isinstance(exit_code, int) or exit_code != 0:
        return {"action": "continue", "message": "The latest same-session AirWorker verify did not exit successfully; rerun air_worker verify."}
    if status.get("outcome") != "completed":
        return {"action": "continue", "message": "The latest same-session AirWorker verify did not complete; resolve it and verify again."}
    receipts = status.get("receipts")
    evidence = status.get("evidence")
    has_receipt = isinstance(receipts, (list, tuple)) and any(
        (isinstance(item, str) and bool(item.strip()))
        or (isinstance(item, Mapping) and any(value not in (None, "", [], {}) for value in item.values()))
        for item in receipts
    )
    has_evidence = has_receipt or bool(evidence)
    if not has_evidence:
        return {"action": "continue", "message": "The latest same-session AirWorker verify has no current receipt or evidence; verify again."}
    if not isinstance(status.get("log_path"), str) or not status["log_path"].strip():
        return {"action": "continue", "message": "The latest same-session AirWorker verify has no log_path; verify again."}
    return None


def _sub(event: str, k: Mapping[str, Any]) -> None:
    product, _ = _bound(k)
    if not product:
        return
    with _LOCK:
        _EVENTS.append({
            "event": event,
            "task_id": str(k.get("child_session_id") or k.get("task_id") or k.get("subagent_id") or ""),
            "role": str(k.get("child_role") or k.get("role") or ""),
            "status": str(k.get("child_status") or k.get("status") or ""),
        })
        del _EVENTS[:-_MAX]


def subagent_start(**kwargs: Any) -> None:
    _sub("subagent_start", kwargs)


def subagent_stop(**kwargs: Any) -> None:
    _sub("subagent_stop", kwargs)


def observations() -> list[dict[str, Any]]:
    with _LOCK:
        return [dict(item) for item in _EVENTS]


def _reset_for_tests() -> None:
    global _ENFORCING
    with _LOCK:
        _BINDINGS.clear()
        _STATUS.clear()
        _EVENTS.clear()
        _ENFORCING = False
