"""Exercise the actual Hermes profile ContextVar API, without secrets or a gateway."""
import os
import pathlib
import json
import shutil
import sys
import tempfile
import unittest
from unittest import mock

from test_plugin import load

HERMES_SOURCE = os.environ.get("HERMES_SOURCE_ROOT", "")
if HERMES_SOURCE:
    sys.path.insert(0, HERMES_SOURCE)


@unittest.skipUnless(HERMES_SOURCE and (pathlib.Path(HERMES_SOURCE) / "agent" / "secret_scope.py").is_file(),
                     "set HERMES_SOURCE_ROOT to the installed Hermes source")
class ProfileScope(unittest.TestCase):
    def test_isolated_profile_binary_and_logs(self):
        from agent.secret_scope import set_multiplex_context, reset_multiplex_context, set_secret_scope, reset_secret_scope
        from hermes_constants import set_hermes_home_override, reset_hermes_home_override
        plugin = load()
        with tempfile.TemporaryDirectory() as td:
            root = pathlib.Path(td)
            product = root / "product"
            product.mkdir()
            a, b, poison = root / "a.exe", root / "b.exe", root / "wrong.exe"
            for path in (a, b, poison):
                path.write_bytes(b"fixture")
            multiplex = set_multiplex_context(True)
            try:
                with mock.patch.dict(os.environ, {"AIR_WORKER_BIN": str(poison), "AIR_WORKER_LOG_DIR": str(root / "wrong-logs")}):
                    with self.assertRaises(plugin.adapter.ProfileContextError):
                        plugin.adapter._binary(product)
                    for name, binary in (("hr", a), ("finance", b)):
                        home = root / name
                        scope = set_secret_scope({"AIR_WORKER_BIN": str(binary)}, profile_home=str(home))
                        override = set_hermes_home_override(home)
                        try:
                            self.assertEqual(str(binary.resolve()), plugin.adapter._binary(product))
                            self.assertEqual(home / "plugin-data" / "air-worker" / "logs", plugin.adapter._log_dir())
                        finally:
                            reset_hermes_home_override(override)
                            reset_secret_scope(scope)
                    scope = set_secret_scope({}, profile_home=str(root / "finance"))
                    try:
                        with mock.patch.dict(os.environ, {"LOCALAPPDATA": str(root / "no-install")}), \
                             mock.patch.object(plugin.adapter.shutil, "which", return_value=None):
                            with self.assertRaises(FileNotFoundError):
                                plugin.adapter._binary(product)
                        if os.name == "nt":
                            installed = root / "default" / "air-worker" / "bin" / "air-worker.exe"
                            installed.parent.mkdir(parents=True)
                            installed.write_bytes(b"fixture")
                            with mock.patch.dict(os.environ, {"LOCALAPPDATA": str(root / "default")}), \
                                 mock.patch.object(plugin.adapter.shutil, "which", return_value=None):
                                self.assertEqual(str(installed.resolve()), plugin.adapter._binary(product))
                    finally:
                        reset_secret_scope(scope)
            finally:
                reset_multiplex_context(multiplex)

    def test_profile_without_override_uses_real_installed_layout(self):
        real = pathlib.Path(os.environ.get("AIR_WORKER_TEST_BIN", ""))
        if os.name != "nt" or not real.is_file():
            self.skipTest("set AIR_WORKER_TEST_BIN to a real Windows CLI")
        from agent.secret_scope import set_multiplex_context, reset_multiplex_context, set_secret_scope, reset_secret_scope
        from hermes_constants import set_hermes_home_override, reset_hermes_home_override
        plugin = load()
        plugin.hooks._reset_for_tests()
        with tempfile.TemporaryDirectory() as td:
            root = pathlib.Path(td)
            product = root / "product"
            product.mkdir()
            (product / "PLAN.md").write_text("# Plan\n- [x] script Done :: echo ok\n", encoding="utf-8")
            (product / "run-config.json").write_text('{"plan":"PLAN.md"}', encoding="utf-8")
            local = root / "local"
            installed = local / "air-worker" / "bin" / "air-worker.exe"
            installed.parent.mkdir(parents=True)
            shutil.copyfile(real, installed)
            home = root / "finance"
            multiplex = set_multiplex_context(True)
            scope = set_secret_scope({}, profile_home=str(home))
            override = set_hermes_home_override(home)
            try:
                with mock.patch.dict(os.environ, {"LOCALAPPDATA": str(local), "PATH": "", "AIR_WORKER_BIN": str(root / "wrong.exe")}):
                    result = json.loads(plugin.execute({"action":"status", "product":str(product)}, session_id="finance-s"))
                self.assertNotEqual("binary_not_found", result.get("stop_reason"), result)
                self.assertEqual("completed", result.get("outcome"), result)
                self.assertTrue((home / "plugin-data" / "air-worker" / "logs").is_dir())
            finally:
                reset_hermes_home_override(override)
                reset_secret_scope(scope)
                reset_multiplex_context(multiplex)
