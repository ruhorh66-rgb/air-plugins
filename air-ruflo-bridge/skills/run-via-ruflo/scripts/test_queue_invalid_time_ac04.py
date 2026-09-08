from __future__ import annotations

import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import test_approve_queue as helpers  # noqa: E402


def test_invalid_created_at_does_not_break_summary() -> None:
    out = helpers._summary([
        {"id": "bad", "status": "pending", "title": "bad-created", "created_at": "oops"},
        {"id": "good", "status": "pending", "title": "live-created", "age_min": 1},
    ])
    assert "live-created" in out
    assert "Ждёт решения: 1" in out


if __name__ == "__main__":
    test_invalid_created_at_does_not_break_summary()
    print("ok  invalid created_at does not break summary")
