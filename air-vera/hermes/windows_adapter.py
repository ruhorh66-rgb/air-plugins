"""Bounded Windows Hermes -> accepted native AirVera transport.

This is NOT a mail/DAV provider implementation, authority grant, or generic
shell command. It reads the product-owned accepted journal using a pinned
installed 0.2.9 bootstrap, then invokes only recognized read-only CLI verbs.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess

# The installed Windows 0.2.9 remains a rollback anchor when 0.2.10 is
# accepted. Its native release-status reads the append-only release journal;
# product/accepted.json is a known stale legacy generation (0.2.5).
PRODUCT = Path(r"R:\-4-\air-vera\product")
BOOTSTRAP_VERSION = "0.2.9"
BOOTSTRAP_SHA256 = "a040a43f95794a49b5575e0cf55da4db1f036f6b7a23356acc85b453add574a0"
TASK_REGISTRY = Path(r"F:\-5-\010_Task_Control_Platform\00_REGISTRY\obsidian_primary_tasks.csv")
TASK_PROOF = Path(
    r"F:\-5-\010_Task_Control_Platform\03_REVIEW"
    r"\COMMERCIAL_MAILOUT_20261013\MAILOUT_PROOF_20261013_V1.json"
)
VERSION = re.compile(r"0\.[0-9]+\.[0-9]+\Z")
MAX_OUTPUT = 65536
# The journal sha256 is a release ZIP digest, never an EXE digest.
# Future 0.2.10 pin MUST be provided only from the signed 0.2.10 EXE.
# Until then any 0.2.10 accepted state fails closed, even for status.
PINNED_EXE_SHA256 = {"0.2.9": BOOTSTRAP_SHA256, "0.2.10": "2b40536f2e84e75c8ebea01a4129cb4f6a2754d5f9ac2b8d95a24cb230bddbf4"}
WINDOWS_REPARSE_POINT = 0x400


class NativeHold(Exception):
    """The exact native identity or read-only result was not established."""


def _not_available(reason: str, **extra) -> dict:
    return {"status": "NOT_AVAILABLE", "reason": reason, **extra}


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(65536), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _env(root: Path) -> dict[str, str]:
    # Do not inherit Hermes provider secrets, alternate executors, runtime
    # overrides, credentials, account identifiers, or arbitrary AIRVERA_*.
    # Keep only OS subprocess launch primitives and exact native AirVera roots.
    allow = {
        "SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "COMSPEC",
        "PATH", "PATHEXT", "TEMP", "TMP",
    }
    result = {
        key: value for key, value in os.environ.items()
        if key.upper() in allow and bool(value)
    }
    result["AIR_VERA_INSTALL_ROOT"] = str(root)
    result["AIR_VERA_AUTOMATION_RUNTIME"] = str(root.parent / "runtime")
    return result


def _check_pinned_exe(root: Path, exe: Path, version: str) -> None:
    """Never execute accepted EXEs from unpinned or reparse-controlled paths."""
    # Windows Python 3.11 has no Path.is_junction(); use lstat file flags.
    for part in (root, root / "versions", root / "versions" / version, exe):
        try:
            info = part.lstat()
        except (OSError, ValueError) as exc:
            raise NativeHold("ACCEPTED_PATH_UNVERIFIABLE") from exc
        if stat.S_ISLNK(info.st_mode):
            raise NativeHold("ACCEPTED_REPARSE_POINT")
        if os.name == "nt":
            attrs = getattr(info, "st_file_attributes", None)
            if attrs is None:
                raise NativeHold("ACCEPTED_REPARSE_ATTRIBUTES_UNAVAILABLE")
            if attrs & WINDOWS_REPARSE_POINT:
                raise NativeHold("ACCEPTED_REPARSE_POINT")
    if not exe.is_file():
        raise NativeHold("ACCEPTED_BINARY_UNAVAILABLE")
    # The fixture override of BOOTSTRAP_SHA256 is deliberate: tests must still
    # run with a synthetic bootstrap while production uses the compile-time SHA.
    pinned = BOOTSTRAP_SHA256 if version == BOOTSTRAP_VERSION else PINNED_EXE_SHA256.get(version)
    if not isinstance(pinned, str) or re.fullmatch(r"[0-9a-fA-F]{64}", pinned) is None:
        raise NativeHold("ACCEPTED_BINARY_SHA_UNPINNED")
    if _sha256(exe).lower() != pinned.lower():
        raise NativeHold("ACCEPTED_BINARY_SHA_MISMATCH")


def _cli(exe: Path, args: list[str], root: Path) -> dict:
    try:
        result = subprocess.run(
            [str(exe), *args], shell=False, cwd=str(exe.parent), env=_env(root),
            capture_output=True, text=True, encoding="utf-8",
            errors="replace", timeout=30, check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise NativeHold("NATIVE_EXEC_UNAVAILABLE") from exc
    if len(result.stdout.encode("utf-8")) > MAX_OUTPUT:
        raise NativeHold("NATIVE_OUTPUT_EXCEEDS_BOUND")
    try:
        data = json.loads(result.stdout)
    except (ValueError, TypeError) as exc:
        raise NativeHold("NATIVE_JSON_UNVERIFIABLE") from exc
    if not isinstance(data, dict):
        raise NativeHold("NATIVE_JSON_NOT_OBJECT")
    if result.returncode:
        # Even a JSON error with a task ID is NOT proof of completion.
        return _not_available("NATIVE_NONZERO_EXIT", native_exit_code=result.returncode)
    return data


def _read_accepted(root: Path) -> tuple[dict, Path]:
    anchor = root / "versions" / BOOTSTRAP_VERSION / "air-vera.exe"
    try:
        _check_pinned_exe(root, anchor, BOOTSTRAP_VERSION)
    except NativeHold as exc:
        if str(exc) == "ACCEPTED_BINARY_SHA_MISMATCH":
            raise NativeHold("PINNED_BOOTSTRAP_UNVERIFIABLE") from exc
        raise
    status = _cli(anchor, ["release-status", "--root", str(root), "--json"], root)
    accepted = status.get("accepted")
    if status.get("status") != "PASS" or not isinstance(accepted, dict):
        raise NativeHold("ACCEPTED_JOURNAL_NOT_PASS")
    version = accepted.get("version")
    if (
        accepted.get("product") != "air-vera"
        or not isinstance(version, str)
        or not VERSION.fullmatch(version)
    ):
        raise NativeHold("ACCEPTED_IDENTITY_INVALID")
    expected_root = root / "versions" / version
    target = accepted.get("runtime_root")
    if (
        not isinstance(target, str)
        or os.path.normcase(os.path.abspath(target))
        != os.path.normcase(os.path.abspath(str(expected_root)))
        or not expected_root.is_dir()
    ):
        raise NativeHold("ACCEPTED_ROOT_UNTRUSTED")
    exe = expected_root / "air-vera.exe"
    _check_pinned_exe(root, exe, version)
    # A valid artifact/journal ZIP hash is not an executable identity pin.
    if not isinstance(accepted.get("sha256"), str) or not re.fullmatch(r"[0-9a-f]{64}", accepted["sha256"]):
        raise NativeHold("ACCEPTED_ARTIFACT_DIGEST_INVALID")
    identity = _cli(exe, ["version", "--json"], root)
    if (
        identity.get("status") != "PASS"
        or identity.get("version") != version
        or identity.get("accepted_version") != version
        or os.path.normcase(str(identity.get("bundle_root", "")))
        != os.path.normcase(str(expected_root))
    ):
        raise NativeHold("ACCEPTED_BINARY_IDENTITY_MISMATCH")
    return accepted, exe


def execute_action(action: str, task_id: str | None = None, *, root: Path = PRODUCT) -> dict:
    """Return one JSON-compatible read-only result, or explicit NOT_AVAILABLE."""
    try:
        accepted, binary = _read_accepted(root)
        version = accepted["version"]
        if action == "task-evidence":
            if version == "0.2.9":
                return _not_available(
                    "FEATURE_NOT_INSTALLED", accepted_version=version,
                    available_from="0.2.10",
                )
            if version != "0.2.10":
                return _not_available("FEATURE_VERSION_NOT_REVIEWED", accepted_version=version)
            args = [
                "task", "evidence", "--task-id", task_id,
                "--registry", str(TASK_REGISTRY),
                "--mail-proof", str(TASK_PROOF), "--json",
            ]
            health = _cli(binary, ["package-health", "--json"], root)
            if health.get("status") != "PASS":
                raise NativeHold("ACCEPTED_PACKAGE_HEALTH_NOT_PASS")
        else:
            args = [action, "--json"]
        result = _cli(binary, args, root)
        latest, _ = _read_accepted(root)
        if any(accepted.get(k) != latest.get(k) for k in ("version", "sha256", "runtime_root")):
            raise NativeHold("ACCEPTED_RELEASE_CHANGED_DURING_READ")
        if action == "task-evidence":
            if result.get("status") != "REVIEW_REQUIRED":
                raise NativeHold("TASK_EVIDENCE_NOT_PROVEN")
            if (
                result.get("task_id") != task_id
                or result.get("no_retry") is not True
                or result.get("task_completed") is not False
                or result.get("employee_delivery_proven") is not False
                or result.get("provider_effects") != 0
                or result.get("task_registry_writes") != 0
            ):
                raise NativeHold("TASK_EVIDENCE_RESULT_MISMATCH")
        result["accepted_version"] = version
        result["host_transport"] = "hermes-native-windows-v1"
        return result
    except (OSError, NativeHold, ValueError) as exc:
        reason = str(exc)
        if not re.fullmatch(r"[A-Z0-9_]+", reason):
            reason = "NATIVE_IDENTITY_UNVERIFIABLE"
        return _not_available(reason)
