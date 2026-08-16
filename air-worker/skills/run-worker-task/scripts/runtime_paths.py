"""Shared host-neutral runtime path resolution for the AIR worker adapters."""
from __future__ import annotations

import os
from pathlib import Path, PurePosixPath

LEGACY_RUNTIME = Path(r"E:\-4-\air-worker")


def resolve_runtime_root(env: dict[str, str] | None = None,
                         system: str | None = None) -> Path:
    """Resolve runtime state without baking an AIR workstation drive into code.

    Precedence is explicit override, documented ``AIR_RUNTIME_ROOT``, then the
    platform state directory.  The old E: location is considered only when it
    already contains state, which permits migration reads without making it the
    default for new hosts.
    """
    values = os.environ if env is None else env
    os_name = system or os.name
    path_type = PurePosixPath if system == "posix" else Path
    explicit = str(values.get("AIR_WORKER_RUNTIME", "")).strip()
    if explicit:
        return Path(explicit)
    documented = str(values.get("AIR_RUNTIME_ROOT", "")).strip()
    if documented:
        return Path(documented)

    if os_name == "nt":
        base = values.get("LOCALAPPDATA") or values.get("APPDATA")
        platform_root = (Path(base) if base else Path.home() / "AppData" / "Local") / "air-worker"
        legacy = Path(values.get("AIR_WORKER_LEGACY_RUNTIME", str(LEGACY_RUNTIME)))
        if (legacy / "worker.sqlite3").is_file() or (legacy / "protocol.jsonl").is_file():
            return legacy
        return platform_root

    base = values.get("XDG_STATE_HOME")
    if base:
        platform_root = path_type(base) / "air-worker"
    elif system == "posix":
        platform_root = PurePosixPath("~/.local/state").expanduser() / "air-worker"
    else:
        platform_root = Path.home() / ".local" / "state" / "air-worker"
    return platform_root


def runtime_layout(root: Path) -> dict[str, Path]:
    return {
        "root": root,
        "db": root / "worker.sqlite3",
        "lock": root / "listener.lock.json",
        "offset": root / "offset.txt",
        "out": root / "out",
        "metrics": root / "run_metrics.csv",
        "protocol": root / "protocol.jsonl",
    }
