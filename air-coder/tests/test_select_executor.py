from __future__ import annotations

import importlib.util
import json
import tempfile
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
SELECTOR = ROOT / "skills" / "route-coding-task" / "scripts" / "select_executor.py"
spec = importlib.util.spec_from_file_location("aircoder_selector", SELECTOR)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(module)


class SelectorTests(unittest.TestCase):
    def test_analysis_stays_chatgpt_rdc(self) -> None:
        result = module.select_executor({
            "mode": "analysis",
            "size": "large",
            "substantial_signs": ["architecture_change"],
        })
        self.assertEqual("chatgpt_rdc", result["route"])

    def test_substantial_implementation_uses_ruflo(self) -> None:
        result = module.select_executor({
            "mode": "implementation",
            "size": "small",
            "substantial_signs": ["new_integration"],
        })
        self.assertEqual("ruflo", result["route"])

    def test_native_cli_when_speed_is_worth_quota(self) -> None:
        result = module.select_executor({
            "mode": "implementation",
            "size": "small",
            "repo_hands_priority": "high",
            "scarce_quota_policy": "speed",
            "native_preference": "codex",
        })
        self.assertEqual("native_cli", result["route"])
        self.assertIn("codex", result["handoff"]["target"])

    def test_conserve_defaults_to_chatgpt_rdc(self) -> None:
        result = module.select_executor({
            "mode": "implementation",
            "size": "small",
            "repo_hands_priority": "high",
            "scarce_quota_policy": "conserve",
        })
        self.assertEqual("chatgpt_rdc", result["route"])

    def test_lpr_override_wins(self) -> None:
        result = module.select_executor({
            "mode": "analysis",
            "lpr_route": "native_cli",
            "native_preference": "claude",
        })
        self.assertEqual("native_cli", result["route"])
        self.assertEqual(["explicit LPR route override"], result["reasons"])

    def test_manifest_versions_match(self) -> None:
        files = [
            ROOT / "product.json",
            ROOT / "capabilities.json",
            ROOT / ".claude-plugin" / "plugin.json",
            ROOT / ".codex-plugin" / "plugin.json",
        ]
        versions = {json.loads(path.read_text(encoding="utf-8"))["version"] for path in files}
        self.assertEqual({"0.1.0-beta.2"}, versions)

    def test_product_contract_paths_exist(self) -> None:
        product = json.loads((ROOT / "product.json").read_text(encoding="utf-8"))
        self.assertTrue((ROOT / product["entry_skill"]).is_file())
        self.assertTrue((ROOT / product["living_document"]).is_file())
        for path in product["contracts"].values():
            self.assertTrue((ROOT / path).is_file(), path)

    def test_load_payload_accepts_utf8_bom(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "task.json"
            path.write_bytes(b"\xef\xbb\xbf" + b'{"mode":"implementation","size":"small"}')
            payload = module._load_payload(str(path))
            self.assertEqual("small", payload["size"])

    def test_invalid_size_fails_closed(self) -> None:
        with self.assertRaises(ValueError):
            module.select_executor({"size": "huge"})


if __name__ == "__main__":
    unittest.main()
