from __future__ import annotations

import contextlib
import io
import json
import os
import tempfile

import approve_via_telegram as module


def test_malformed_request_status_is_unverifiable() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "broken.json")
        with open(path, "w", encoding="utf-8") as fh:
            fh.write('{"id":"broken"')
        previous = module.QUEUE
        module.QUEUE = tmp
        out = io.StringIO()
        try:
            with contextlib.redirect_stdout(out):
                code = module.status(["broken"])
        finally:
            module.QUEUE = previous
        payload = json.loads(out.getvalue())
        assert code == 2
        assert payload["status"] == "unverifiable"


if __name__ == "__main__":
    test_malformed_request_status_is_unverifiable()
    print("ok  malformed request status is unverifiable")
