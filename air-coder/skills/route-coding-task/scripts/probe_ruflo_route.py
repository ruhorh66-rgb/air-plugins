from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve()
AIR_CODER_ROOT = HERE.parents[3]
DEFAULT_PROFILE = AIR_CODER_ROOT / "contracts" / "ruflo-route-profile.json"
DEFAULT_BRIDGE_ROOT = Path(r"E:\-7-\air-ruflo-bridge")
DEFAULT_HIVE_ROOT = Path(r"E:\-4-\ruflo-hive")
DEFAULT_PILOT_ROOT = Path(r"E:\-4-\ruflo-pilot")


def _read_json(path: Path) -> dict[str, Any]:
    data = json.loads(path.read_text(encoding="utf-8-sig"))
    if not isinstance(data, dict):
        raise ValueError(f"expected JSON object: {path}")
    return data


def _cli_candidates(pilot_root: Path, version: str) -> list[Path]:
    root = pilot_root / f".npm-cache-{version}"
    return sorted(root.glob("_npx/*/node_modules/@claude-flow/cli/bin/cli.js"))

def collect_facts(
    profile: dict[str, Any],
    bridge_root: Path,
    hive_root: Path,
    pilot_root: Path,
) -> dict[str, Any]:
    engine_path = hive_root / "engine.json"
    mcp_path = hive_root / ".mcp.json"
    run_config_path = hive_root / "run-config.json"
    engine = _read_json(engine_path)
    required = str(profile["engine"]["required_version"])
    rollback = str(profile["engine"]["rollback_version"])
    cli = _cli_candidates(pilot_root, required)
    rollback_cli = _cli_candidates(pilot_root, rollback)
    selected_cli = cli[0] if cli else None
    mcp = _read_json(mcp_path) if mcp_path.is_file() else {}
    ruflo_args = (((mcp.get("mcpServers") or {}).get("ruflo") or {}).get("args") or [])
    mcp_cli = Path(str(ruflo_args[0])) if ruflo_args else None
    bridge_script = bridge_root / str(profile["bridge"]["script"])
    booster_target = hive_root / "node_modules" / "agentic-flow"
    booster_link = selected_cli.parents[3] / "agentic-flow" if selected_cli else None
    booster_same = False
    if booster_link and booster_link.exists() and booster_target.exists():
        try:
            booster_same = os.path.samefile(booster_link, booster_target)
        except OSError:
            booster_same = False
    return {
        "engine_path": str(engine_path),
        "engine_version": str(engine.get("version", "")),
        "engine_rollback": str(engine.get("rollback", "")),
        "required_version": required,
        "required_cli_count": len(cli),
        "required_cli": str(selected_cli) if selected_cli else None,
        "rollback_cli_count": len(rollback_cli),
        "mcp_path": str(mcp_path),
        "mcp_exists": mcp_path.is_file(),
        "mcp_points_to_required_engine": bool(selected_cli and mcp_cli and os.path.normcase(str(mcp_cli)) == os.path.normcase(str(selected_cli))),
        "run_config_exists": run_config_path.is_file(),
        "bridge_script": str(bridge_script),
        "bridge_script_exists": bridge_script.is_file(),
        "booster_link": str(booster_link) if booster_link else None,
        "booster_target": str(booster_target),
        "booster_samefile": booster_same,
    }


def evaluate_facts(facts: dict[str, Any]) -> dict[str, Any]:
    checks = {
        "engine_version": facts["engine_version"] == facts["required_version"],
        "rollback_version": bool(facts["engine_rollback"]),
        "engine_cli": facts["required_cli_count"] >= 1,
        "rollback_cli": facts["rollback_cli_count"] >= 1,
        "mcp_exists": bool(facts["mcp_exists"]),
        "mcp_engine_sync": bool(facts["mcp_points_to_required_engine"]),
        "run_config": bool(facts["run_config_exists"]),
        "bridge_script": bool(facts["bridge_script_exists"]),
        "booster_link": bool(facts["booster_samefile"]),
    }
    failed = [name for name, passed in checks.items() if not passed]
    return {
        "status": "PASS" if not failed else "FAIL",
        "checks": checks,
        "failed": failed,
        "facts": facts,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Fail-closed Ruflo handoff preflight for AirCoder")
    parser.add_argument("--profile", default=str(DEFAULT_PROFILE))
    parser.add_argument("--bridge-root", default=str(DEFAULT_BRIDGE_ROOT))
    parser.add_argument("--hive-root", default=str(DEFAULT_HIVE_ROOT))
    parser.add_argument("--pilot-root", default=str(DEFAULT_PILOT_ROOT))
    parser.add_argument("--pretty", action="store_true")
    args = parser.parse_args()
    try:
        profile = _read_json(Path(args.profile))
        facts = collect_facts(profile, Path(args.bridge_root), Path(args.hive_root), Path(args.pilot_root))
        result = evaluate_facts(facts)
    except (OSError, ValueError, KeyError, json.JSONDecodeError) as exc:
        result = {"status": "FAIL", "checks": {}, "failed": ["probe_exception"], "error": str(exc)}
    print(json.dumps(result, ensure_ascii=False, indent=2 if args.pretty else None))
    return 0 if result["status"] == "PASS" else 2


if __name__ == "__main__":
    raise SystemExit(main())
