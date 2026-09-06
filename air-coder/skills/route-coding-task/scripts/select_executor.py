from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any

ROUTES = {"chatgpt_rdc", "ruflo", "native_cli"}
SIZES = {"small", "medium", "large"}
MODES = {"analysis", "implementation"}
QUOTA_POLICIES = {"conserve", "balanced", "speed"}
HANDS_PRIORITIES = {"normal", "high"}
NATIVE_PREFERENCES = {"auto", "codex", "claude"}
SUBSTANTIAL_SIGNS = {
    "new_subsystem",
    "new_integration",
    "migration",
    "wide_release",
    "cross_repo",
    "architecture_change",
    "parallel_roles",
}


def _require_choice(payload: dict[str, Any], key: str, allowed: set[str], default: str) -> str:
    value = str(payload.get(key, default))
    if value not in allowed:
        raise ValueError(f"{key} must be one of {sorted(allowed)}; got {value!r}")
    return value


def _substantial_signs(payload: dict[str, Any]) -> list[str]:
    raw = payload.get("substantial_signs", [])
    if not isinstance(raw, list):
        raise ValueError("substantial_signs must be a JSON list")
    signs = [str(item) for item in raw]
    unknown = sorted(set(signs) - SUBSTANTIAL_SIGNS)
    if unknown:
        raise ValueError(f"unknown substantial_signs: {unknown}")
    return signs


def _handoff(route: str, native_preference: str) -> dict[str, str]:
    if route == "chatgpt_rdc":
        return {"target": "ChatGPT + RDC", "probe": "check RDC availability"}
    if route == "ruflo":
        return {"target": "air-ruflo-bridge:run-via-ruflo", "probe": "check documented Ruflo runtime"}
    return {"target": f"native CLI ({native_preference})", "probe": "check codex/claude CLI"}


def select_executor(payload: dict[str, Any]) -> dict[str, Any]:
    mode = _require_choice(payload, "mode", MODES, "implementation")
    size = _require_choice(payload, "size", SIZES, "small")
    quota = _require_choice(payload, "scarce_quota_policy", QUOTA_POLICIES, "conserve")
    hands = _require_choice(payload, "repo_hands_priority", HANDS_PRIORITIES, "normal")
    native_preference = _require_choice(payload, "native_preference", NATIVE_PREFERENCES, "auto")
    signs = _substantial_signs(payload)

    override = payload.get("lpr_route")
    if override is not None:
        override = str(override)
        if override not in ROUTES:
            raise ValueError(f"lpr_route must be one of {sorted(ROUTES)}")
        route = override
        reasons = ["explicit LPR route override"]
    elif mode == "analysis":
        route = "chatgpt_rdc"
        reasons = ["analysis-only work favors strong reasoning without scarce CLI quota"]
    elif signs or size == "large":
        route = "ruflo"
        reasons = ["substantial implementation should use Ruflo"]
        if signs:
            reasons.append("substantial signs: " + ", ".join(sorted(signs)))
    elif hands == "high" and quota in {"balanced", "speed"}:
        route = "native_cli"
        reasons = ["fast repository hands are worth scarce native CLI quota"]
    else:
        route = "chatgpt_rdc"
        reasons = ["small/local work does not justify Ruflo overhead or scarce native CLI quota"]

    return {
        "route": route,
        "reasons": reasons,
        "handoff": _handoff(route, native_preference),
        "inputs": {
            "mode": mode,
            "size": size,
            "substantial_signs": signs,
            "scarce_quota_policy": quota,
            "repo_hands_priority": hands,
            "native_preference": native_preference,
        },
        "result_contract": "contracts/run-result.schema.json",
    }


def _load_payload(path: str) -> dict[str, Any]:
    text = Path(path).read_text(encoding="utf-8") if path != "-" else __import__("sys").stdin.read()
    data = json.loads(text)
    if not isinstance(data, dict):
        raise ValueError("input JSON must be an object")
    return data


def main() -> int:
    parser = argparse.ArgumentParser(description="Select an AirCoder execution route")
    parser.add_argument("--input", required=True, help="Task facts JSON file or '-' for stdin")
    parser.add_argument("--pretty", action="store_true")
    args = parser.parse_args()
    try:
        decision = select_executor(_load_payload(args.input))
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        parser.error(str(exc))
    print(json.dumps(decision, ensure_ascii=False, indent=2 if args.pretty else None))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
