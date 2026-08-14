"""Отцепленный запуск не должен задерживать следующий Telegram callback.

Регрессия ERR-2026-000233/000242: прежде первый `_run_request` ждал `run_task.ps1`
часами, поэтому второй callback не доходил даже до `answerCallbackQuery`. Здесь
настоящий poll_once вызывает настоящий _run_request; подменены только Telegram,
PowerShell и запись в реестр очереди.
"""
from __future__ import annotations

import hashlib
import io
import json
import os
import sys
import tempfile
import time
from contextlib import redirect_stdout

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import approve_listener as al  # noqa: E402
import approve_via_telegram as avt  # noqa: E402


def _request(tmp: str, rid: str) -> dict:
    objective = os.path.join(tmp, f"{rid}.md")
    with open(objective, "w", encoding="utf-8") as fh:
        fh.write(f"# {rid}\n")
    req = {
        "id": rid,
        "title": f"TASK-OBS-{rid[-4:]} (очередь роя)",
        "report": os.path.join(tmp, f"{rid}.report.md"),
        "objective_file": objective,
        "objective_sha256": hashlib.sha256(open(objective, "rb").read()).hexdigest(),
        "target_path": tmp,
        "workers": 3,
        "priority": "high",
        "created_at": time.time(),
        "status": "pending",
    }
    req["sig"] = avt.sign_request(req)
    return req


def test_second_button_is_acknowledged_while_first_detached_run_is_running():
    """Негативный контроль: до исправления второй poll_once не возвращался бы."""
    launched, api_calls = [], []
    with tempfile.TemporaryDirectory() as tmp:
        first, second = _request(tmp, "test0001"), _request(tmp, "test0002")
        for req in (first, second):
            with open(os.path.join(tmp, f"{req['id']}.json"), "w", encoding="utf-8") as fh:
                json.dump(req, fh)
        updates = iter([
            {"ok": True, "result": [{
                "update_id": 1,
                "callback_query": {"id": "first", "data": "run:test0001",
                                   "from": {"id": "42"},
                                   "message": {"message_id": 100}},
            }]},
            {"ok": True, "result": [{
                "update_id": 2,
                "callback_query": {"id": "second", "data": "run:test0002",
                                   "from": {"id": "42"},
                                   "message": {"message_id": 101}},
            }]},
        ])

        saved = (al.QUEUE, al._api, al._notify, al._queue_record, al._hive_busy,
                 al._load_offset, al._save_offset, al.subprocess.Popen,
                 al._cycle, al._first_poll_done, al._fail_streak, al._last_ok_at,
                 al.HEARTBEAT_EVERY)
        al.QUEUE = tmp
        al._api = lambda method, payload=None, timeout=70: (
            api_calls.append((method, payload)) or (next(updates) if method == "getUpdates"
                                                     else {"ok": True}))
        al._notify = lambda text, **_kw: None
        al._queue_record = lambda *_a, **_kw: ""
        al._hive_busy = lambda: False
        al._load_offset = lambda: 0
        al._save_offset = lambda _value: None
        al.subprocess.Popen = lambda argv, *a, **kw: launched.append((argv, kw)) or object()
        al._cycle = 0
        al._first_poll_done = True
        al._fail_streak = 0
        al._last_ok_at = time.time()
        al.HEARTBEAT_EVERY = 2
        try:
            log = io.StringIO()
            with redirect_stdout(log):
                al.poll_once("42")
                al.poll_once("42")
        finally:
            (al.QUEUE, al._api, al._notify, al._queue_record, al._hive_busy,
             al._load_offset, al._save_offset, al.subprocess.Popen,
             al._cycle, al._first_poll_done, al._fail_streak, al._last_ok_at,
             al.HEARTBEAT_EVERY) = saved

        with open(os.path.join(tmp, "test0001.json"), encoding="utf-8") as fh:
            first_after = json.load(fh)
        with open(os.path.join(tmp, "test0002.json"), encoding="utf-8") as fh:
            second_after = json.load(fh)

    answered = [payload["callback_query_id"] for method, payload in api_calls
                if method == "answerCallbackQuery"]
    assert answered == ["first", "second"], answered
    assert first_after["run_state"] == "running"
    assert second_after["status"] == "approved"
    assert second_after["run_state"] == "waiting"
    assert len(launched) == 1, "второй callback не должен обогнать первый до hive.lock"
    assert "жив, цикл 2" in log.getvalue(), "heartbeat пропал во время detached run"


def test_exit_marker_is_recorded_after_listener_restart():
    """Marker не зависит от объекта Popen и потому переживает рестарт listener."""
    notifications, queue_calls = [], []
    with tempfile.TemporaryDirectory() as tmp:
        stdout = os.path.join(tmp, "_run_test0003.stdout.log")
        exit_path = os.path.join(tmp, "_run_test0003.exit")
        with open(stdout, "w", encoding="utf-8") as fh:
            fh.write("Рой работал штатно\n")
        with open(exit_path, "w", encoding="utf-8") as fh:
            fh.write("0\n")
        req = {
            "id": "test0003",
            "title": "TASK-OBS-0003 (очередь роя)",
            "report": os.path.join(tmp, "report.md"),
            "status": "approved",
            "run_state": "running",
            "run_started_at": time.time() - 2,
            "run_stdout": stdout,
            "run_exit": exit_path,
        }
        with open(os.path.join(tmp, "test0003.json"), "w", encoding="utf-8") as fh:
            json.dump(req, fh)
        saved = al.QUEUE, al._queue_record, al._notify, al._push_next
        al.QUEUE = tmp
        al._queue_record = lambda *args: queue_calls.append(args) or ""
        al._notify = lambda text, **_kw: notifications.append(text)
        al._push_next = lambda: None
        try:
            al._service_runs()
        finally:
            al.QUEUE, al._queue_record, al._notify, al._push_next = saved
        with open(os.path.join(tmp, "test0003.json"), encoding="utf-8") as fh:
            after = json.load(fh)

    assert queue_calls and queue_calls[0][1] == "done", queue_calls
    assert any("Рой отработал" in text for text in notifications)
    assert after["run_state"] == "finished"
    assert after["run_returncode"] == 0
