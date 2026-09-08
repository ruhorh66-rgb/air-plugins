from __future__ import annotations

import os
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import approve_listener as module  # noqa: E402


def test_nonobject_request_is_rejected_without_crashing() -> None:
    answers: list[str] = []
    with tempfile.TemporaryDirectory() as tmp:
        open(os.path.join(tmp, "bad.json"), "w", encoding="utf-8").write("[]")
        saved = (module.QUEUE, module._api, module._load_offset, module._save_offset,
                 module._answer, module._notify, module._first_poll_done)
        module.QUEUE = tmp
        module._load_offset = lambda: 0
        module._save_offset = lambda value: None
        module._answer = lambda _id, text: answers.append(text)
        module._notify = lambda *a, **k: None
        module._first_poll_done = True
        module._api = lambda *a, **k: {"ok": True, "result": [{"update_id": 1,
            "callback_query": {"id": "cb", "from": {"id": "123"},
                               "data": "run:bad", "message": {"message_id": 1}}}]}
        try:
            handled = module.poll_once("123")
        finally:
            (module.QUEUE, module._api, module._load_offset, module._save_offset,
             module._answer, module._notify, module._first_poll_done) = saved
        assert handled == 0
        assert answers


if __name__ == "__main__":
    test_nonobject_request_is_rejected_without_crashing()
    print("ok  non-object request rejected without crashing")
