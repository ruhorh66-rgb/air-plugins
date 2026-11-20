"""Integration checks against an explicitly selected AirWorker executable."""
import json
import multiprocessing
import os
import pathlib
import tempfile
import unittest
from unittest import mock

from test_plugin import load

BIN = os.environ.get("AIR_WORKER_TEST_BIN", "")


def concurrent_create(root, logs, barrier, queue, alternate):
    p = load()
    p.hooks._reset_for_tests()
    product = pathlib.Path(root)
    raw = str(product / ".." / product.name) if alternate else root
    args = {"action": "plan_new", "product": raw, "title": "Concurrent fixture", "parent": "Stage",
            "owner": "test", "done_when": "done", "request_id": "concurrent-fixture"}
    with mock.patch.dict(os.environ, {"AIR_WORKER_BIN": BIN, "AIR_WORKER_LOG_DIR": logs}):
        barrier.wait(timeout=20)
        queue.put(json.loads(p.execute(args, session_id="fixture-session")))


@unittest.skipUnless(BIN and pathlib.Path(BIN).is_file(), "set AIR_WORKER_TEST_BIN to a real CLI")
class NativeIntegration(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name) / "product"
        self.root.mkdir()
        (self.root / "PLAN.md").write_text("# Plan\n\n## Stage\n- [x] script Legacy :: echo ok\n", encoding="utf-8")
        (self.root / "run-config.json").write_text('{"plan":"PLAN.md"}', encoding="utf-8")
        self.plugin = load()
        self.plugin.hooks._reset_for_tests()
        self.env = mock.patch.dict(os.environ, {"AIR_WORKER_BIN": BIN, "AIR_WORKER_LOG_DIR": str(pathlib.Path(self.temp.name) / "logs")})
        self.env.start()
        self.addCleanup(self.env.stop)

    def request(self, key="fixture"):
        return {"action": "plan_new", "product": str(self.root), "title": "Fixture title", "parent": "Stage",
                "owner": "test", "done_when": "fixture done", "request_id": key}

    def test_create_repeat_and_readback(self):
        first = json.loads(self.plugin.execute(self.request(), session_id="fixture-session"))
        again = json.loads(self.plugin.execute(self.request(), session_id="fixture-session"))
        self.assertEqual("created", first["outcome"], first)
        self.assertEqual(first["node_id"], again.get("node_id"))
        self.assertTrue(again.get("replayed"))
        files = list((self.root / "plan").glob("N-*.md"))
        self.assertEqual(1, len(files))
        self.assertIn("Fixture title", files[0].read_text(encoding="utf-8"))
        self.assertIn("[[" + first["node_id"] + "]]", (self.root / "PLAN.md").read_text(encoding="utf-8"))
        status = json.loads(self.plugin.execute({"action": "status", "product": str(self.root)}, session_id="fixture-session"))
        self.assertEqual("plan_sources_disagree", status["stop_reason"])
        self.assertNotIn("progress", status)

    def test_missing_reserved_numbers_fail_closed(self):
        with (self.root / "PLAN.md").open("a", encoding="utf-8") as stream:
            for n in range(63, 67):
                stream.write(f"- [open] [[N-{n:03d}_reserved]]\n")
        status = json.loads(self.plugin.execute({"action":"status", "product":str(self.root)}, session_id="s"))
        result = json.loads(self.plugin.execute(self.request(), session_id="s"))
        self.assertEqual("plan_sources_disagree", status["stop_reason"])
        self.assertEqual("plan_sources_disagree", result["stop_reason"])
        self.assertFalse(list((self.root / "plan").glob("N-*.md")))

    def test_occupied_node_files_do_not_collide(self):
        (self.root / "plan").mkdir()
        for n in range(63, 67):
            node_id = f"N-{n:03d}_reserved"
            body = (f'---\nid: "{node_id}"\ntitle: "Reserved"\nparent: "Stage"\n'
                    'trigger: "test"\nowner: "test"\ndone_when: "done"\nstatus: "open"\n'
                    'return_to: "Stage"\ncreated_at: "2026-01-01T00:00:00Z"\n'
                    'updated_at: "2026-01-01T00:00:00Z"\nreceipts:\n---\n')
            (self.root / "plan" / (node_id + ".md")).write_text(body, encoding="utf-8")
            with (self.root / "PLAN.md").open("a", encoding="utf-8") as stream:
                stream.write(f"- [open] [[{node_id}]]\n")
        result = json.loads(self.plugin.execute(self.request(), session_id="s"))
        self.assertEqual("created", result.get("outcome"), result)
        self.assertTrue(result["node_id"].startswith("N-067_"), result)
        self.assertEqual(5, len(list((self.root / "plan").glob("N-*.md"))))

    def test_alternate_config_plan_cannot_hide_open_native_node(self):
        created = json.loads(self.plugin.execute(self.request(), session_id="s"))
        self.assertEqual("created", created.get("outcome"), created)
        (self.root / "alternate.md").write_text("# Alternate\n- [x] script Completed :: echo ok\n", encoding="utf-8")
        (self.root / "alternate.json").write_text('{"plan":"alternate.md"}', encoding="utf-8")
        result = json.loads(self.plugin.execute({"action":"status", "product":str(self.root), "config":"alternate.json"}, session_id="s"))
        self.assertEqual("plan_sources_disagree", result.get("stop_reason"), result)
        self.assertNotIn("progress", result)

    def test_alternate_config_cannot_mutate_wrong_plan(self):
        (self.root / "alternate.md").write_text("# Alternate\n\n## Stage\n", encoding="utf-8")
        (self.root / "alternate.json").write_text('{"plan":"alternate.md"}', encoding="utf-8")
        paths = [self.root / "PLAN.md", self.root / "alternate.md", self.root / "alternate.json"]
        before = [p.read_bytes() for p in paths]
        args = dict(self.request(), config="alternate.json")
        result = json.loads(self.plugin.execute(args, session_id="s"))
        self.assertEqual("plan_sources_disagree", result.get("stop_reason"), result)
        self.assertEqual(before, [p.read_bytes() for p in paths])
        self.assertFalse((self.root / ".air-worker" / "hermes-plan-requests").exists())
        self.assertFalse(list((self.root / "plan").glob("N-*.md")))

    def test_default_config_cannot_mutate_wrong_plan(self):
        (self.root / "alternate.md").write_text("# Alternate\n\n## Stage\n", encoding="utf-8")
        (self.root / "run-config.json").write_text('{"plan":"alternate.md"}', encoding="utf-8")
        paths = [self.root / "PLAN.md", self.root / "alternate.md", self.root / "run-config.json"]
        before = [p.read_bytes() for p in paths]
        result = json.loads(self.plugin.execute(self.request(), session_id="s"))
        self.assertEqual("plan_sources_disagree", result.get("stop_reason"), result)
        self.assertEqual(before, [p.read_bytes() for p in paths])
        self.assertFalse((self.root / ".air-worker" / "hermes-plan-requests").exists())
        self.assertFalse(list((self.root / "plan").glob("N-*.md")))

    def test_successful_request_with_alternate_config_does_not_replay_or_mutate(self):
        first = json.loads(self.plugin.execute(self.request(), session_id="s"))
        self.assertEqual("created", first.get("outcome"), first)
        (self.root / "alternate.md").write_text("# Alternate\n\n## Stage\n", encoding="utf-8")
        (self.root / "alternate.json").write_text('{"plan":"alternate.md"}', encoding="utf-8")
        paths = [self.root / "PLAN.md", self.root / "alternate.md", self.root / "alternate.json"]
        before = [p.read_bytes() for p in paths]
        result = json.loads(self.plugin.execute(dict(self.request(), config="alternate.json"), session_id="s"))
        self.assertEqual("plan_sources_disagree", result.get("stop_reason"), result)
        self.assertEqual(before, [p.read_bytes() for p in paths])
        self.assertEqual(1, len(list((self.root / "plan").glob("N-*.md"))))

    def test_busy_operation_does_not_leave_mutation_lock(self):
        self.assertTrue(self.plugin.hooks.begin_operation(str(self.root)))
        try:
            blocked = json.loads(self.plugin.execute(self.request("busy-first"), session_id="s"))
        finally:
            self.plugin.hooks.end_operation(str(self.root))
        self.assertEqual("product_pending_do_not_retry", blocked.get("stop_reason"), blocked)
        self.assertFalse((self.root / ".air-worker" / "hermes-plan-requests" / "product-mutation.lock").exists())
        followup = json.loads(self.plugin.execute(self.request("after-busy"), session_id="s"))
        self.assertEqual("created", followup.get("outcome"), followup)
        self.assertEqual(1, len(list((self.root / "plan").glob("N-*.md"))))

    def test_simultaneous_processes_and_restart(self):
        ctx = multiprocessing.get_context("spawn")
        barrier, queue = ctx.Barrier(2), ctx.Queue()
        logs = str(pathlib.Path(self.temp.name) / "logs")
        workers = [ctx.Process(target=concurrent_create, args=(str(self.root), logs, barrier, queue, bool(i))) for i in range(2)]
        for worker in workers:
            worker.start()
        for worker in workers:
            worker.join(30)
            self.assertEqual(0, worker.exitcode)
        replies = [queue.get(timeout=5), queue.get(timeout=5)]
        self.assertTrue(any(r.get("outcome") == "created" for r in replies), replies)
        self.assertEqual(1, len(list((self.root / "plan").glob("N-*.md"))))
        # Reloading the adapter is not enough: run replay in a fresh spawned process.
        barrier = ctx.Barrier(1)
        replay = ctx.Process(target=concurrent_create, args=(str(self.root), logs, barrier, queue, False))
        replay.start()
        replay.join(30)
        self.assertEqual(0, replay.exitcode)
        self.assertTrue(queue.get(timeout=5).get("replayed"))


if __name__ == "__main__":
    unittest.main()
