"""OpenCode streaming compatibility bridge for AIR LLM Router v0.3.0.

The bridge owns transport only. Provider/model policy, keys and billing remain
inside AIR LLM Router. Every completion is a bounded ``--invoke`` process.
"""
import json
import os
import subprocess
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HOST = "127.0.0.1"
PORT = int(os.environ.get("AIR_ROUTER_BRIDGE_PORT", "18092"))
ROUTER = os.environ["AIR_LLM_ROUTER_PY"]
RECEIPTS = os.environ.get("AIR_ROUTER_BRIDGE_RECEIPTS", "").strip()


def invoke_router(body: dict) -> tuple[int, dict]:
    request = dict(body)
    request.pop("stream", None)
    request["model"] = "air-auto"
    # Router-defined audit mode: preserves _router_routing only in the local
    # response. Router still owns provider/model selection and strips this
    # coordination field before any upstream provider call.
    request["_air_debug"] = True
    raw = json.dumps(request, ensure_ascii=False).encode("utf-8")
    env = dict(os.environ)
    env["PYTHONUTF8"] = "1"
    done = subprocess.run(
        [sys.executable, ROUTER, "--invoke"], input=raw,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env,
        timeout=int(os.environ.get("AIR_ROUTER_INVOKE_TIMEOUT", "180")),
    )
    text = done.stdout.decode("utf-8", errors="replace").strip()
    if not text:
        return 502, {"error": {"message": "router returned no JSON"}}
    wrapper = json.loads(text.splitlines()[-1])
    return int(wrapper.get("status", 502)), wrapper.get("payload") or {}


def route_evidence(payload: dict) -> dict:
    routing = payload.get("_router_routing") or {}
    usage = payload.get("usage") or {}
    choice = ((payload.get("choices") or [{}])[0] or {})
    return {
        "ts": time.time(),
        "status": 200,
        "provider": routing.get("effective_provider", ""),
        "model": routing.get("effective_model", ""),
        "cost": usage.get("cost", 0),
        "finish": choice.get("finish_reason", ""),
    }


def write_receipt(payload: dict) -> None:
    if not RECEIPTS:
        return
    evidence = route_evidence(payload)
    with open(RECEIPTS, "a", encoding="utf-8", newline="\n") as handle:
        handle.write(json.dumps(evidence, ensure_ascii=False) + "\n")


def completion_chunk(payload: dict) -> list[dict]:
    choice = ((payload.get("choices") or [{}])[0] or {})
    message = choice.get("message") or {}
    model = payload.get("model") or "air-auto"
    ident = payload.get("id") or "air-router"
    created = payload.get("created") or int(time.time())
    chunks: list[dict] = []
    base = {"id": ident, "object": "chat.completion.chunk", "created": created, "model": model}
    delta = {"role": "assistant"}
    if message.get("content") is not None:
        delta["content"] = message.get("content")
    if message.get("reasoning") is not None:
        delta["reasoning"] = message.get("reasoning")
    if message.get("tool_calls"):
        delta["tool_calls"] = message.get("tool_calls")
    chunks.append({**base, "choices": [{"index": 0, "delta": delta, "finish_reason": None}]})
    chunks.append({
        **base,
        "choices": [{"index": 0, "delta": {}, "finish_reason": choice.get("finish_reason") or "stop"}],
        "usage": payload.get("usage"),
    })
    return chunks


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt: str, *args) -> None:
        sys.stderr.write("air-worker-router-bridge %s\n" % (fmt % args))

    def _json(self, status: int, payload: dict) -> None:
        raw = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self) -> None:
        if self.path.rstrip("/") == "/api.json":
            self._json(200, {})
            return
        if self.path.rstrip("/") == "/v1/models":
            self._json(200, {"object": "list", "data": [{
                "id": "air-auto", "object": "model", "owned_by": "air-router"
            }]})
            return
        self._json(404, {"error": {"message": "not found"}})

    def do_POST(self) -> None:
        if self.path.rstrip("/") != "/v1/chat/completions":
            self._json(404, {"error": {"message": "not found"}})
            return
        try:
            length = int(self.headers.get("Content-Length") or 0)
            body = json.loads(self.rfile.read(length) or b"{}")
            status, payload = invoke_router(body)
        except Exception as exc:
            self._json(502, {"error": {"message": f"bridge: {type(exc).__name__}: {exc}"}})
            return
        if status != 200:
            self._json(status, payload)
            return
        write_receipt(payload)
        if not body.get("stream"):
            self._json(200, payload)
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream; charset=utf-8")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "close")
        self.end_headers()
        for chunk in completion_chunk(payload):
            raw = json.dumps(chunk, ensure_ascii=False).encode("utf-8")
            self.wfile.write(b"data: " + raw + b"\n\n")
            self.wfile.flush()
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()
        self.close_connection = True


def main() -> int:
    if not os.path.isfile(ROUTER):
        print(f"AIR LLM Router not found: {ROUTER}", file=sys.stderr)
        return 2
    server = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"air-worker router bridge http://{HOST}:{PORT}/v1", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
