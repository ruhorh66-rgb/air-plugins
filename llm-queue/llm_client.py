r"""
Shared client for llama-server (localhost:8080), coordinating concurrent access
across independent scripts/sessions so uncoordinated callers don't oversubscribe
the server's parallel slots and time each other out.

Added 2026-07-11: air-vault-local's l2_extract_local.py / cross_vault_check_local.py
and a separate video-analysis pipeline all call the same llama-server
instance independently, with no shared awareness of each other -- confirmed as a
live concern when a video-analysis session and an air-vault-local retry ran at the
same time. llama-server itself doesn't crash under overload (it queues), but a
caller with a tight client-side timeout can see spurious failures if it queues
behind more in-flight requests than the server's -np slot count.

Not a job broker / not a persistent service -- a file-based semaphore matching
the server's -np flag (see your llama-server start script), same spirit as
a single-run lockfile elsewhere in the contour (
deepseek_web_agent), just N slots instead of 1.

Usage (drop-in replacement for a direct requests.post to /v1/chat/completions):
    from llm_client import call_llm
    text = call_llm(prompt, client_name="l2_extract_local")

To see who's currently holding slots:
    python llm_client.py --status
"""
import argparse
import json
import os
import sys
import time
import uuid
from pathlib import Path

import requests

LLAMA_SERVER_URL = "http://127.0.0.1:8080/v1/chat/completions"
NO_PROXY = {"http": None, "https": None}
MODEL_NAME = "qwen2.5-7b-instruct"

QUEUE_DIR = Path(__file__).resolve().parent
# Slot locks are shared state, not code: every caller of this semaphore must look at the
# SAME directory or there is no coordination at all — two sets of locks means two
# independent pools, which is exactly the oversubscription this module exists to stop.
# LLM_QUEUE_DATA_DIR keeps them with the runtime while the code lives anywhere.
DATA_DIR = Path(os.environ.get("LLM_QUEUE_DATA_DIR", QUEUE_DIR))
SLOTS_DIR = DATA_DIR / "slots"
MAX_SLOTS = int(os.environ.get("LLM_MAX_SLOTS", "2"))  # must match llama-server -np
STALE_LOCK_SECONDS = 30 * 60  # a slot lock older than this with a dead PID is reclaimed


def _pid_alive(pid: int) -> bool:
    if os.name != "nt":
        try:
            os.kill(pid, 0)
            return True
        except OSError:
            return False
    # Windows: tasklist is the simplest reliable check without extra dependencies
    import subprocess
    out = subprocess.run(
        ["tasklist", "/FI", f"PID eq {pid}"], capture_output=True, text=True,
    ).stdout
    return str(pid) in out


def _reclaim_stale_locks() -> None:
    if not SLOTS_DIR.exists():
        return
    for f in SLOTS_DIR.glob("slot_*.lock"):
        try:
            info = json.loads(f.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        age = time.time() - info.get("since", 0)
        if age > STALE_LOCK_SECONDS and not _pid_alive(info.get("pid", -1)):
            f.unlink(missing_ok=True)


class _Slot:
    def __init__(self, path: Path):
        self.path = path

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        self.path.unlink(missing_ok=True)


def acquire_slot(client_name: str, poll_interval: float = 1.0, timeout: float | None = None) -> _Slot:
    SLOTS_DIR.mkdir(parents=True, exist_ok=True)
    job_id = str(uuid.uuid4())[:8]
    start = time.time()
    while True:
        _reclaim_stale_locks()
        for i in range(MAX_SLOTS):
            candidate = SLOTS_DIR / f"slot_{i}.lock"
            try:
                fd = os.open(str(candidate), os.O_CREAT | os.O_EXCL | os.O_WRONLY)
                os.write(fd, json.dumps({
                    "client": client_name, "job_id": job_id,
                    "pid": os.getpid(), "since": time.time(),
                }).encode("utf-8"))
                os.close(fd)
                return _Slot(candidate)
            except FileExistsError:
                continue
        if timeout is not None and (time.time() - start) > timeout:
            raise TimeoutError(f"Could not acquire an LLM slot within {timeout}s (client={client_name})")
        time.sleep(poll_interval)


def call_llm(prompt: str, client_name: str, max_tokens: int = 800,
             temperature: float = 0.1, timeout: float = 180,
             slot_timeout: float | None = None) -> str:
    with acquire_slot(client_name, timeout=slot_timeout):
        resp = requests.post(
            LLAMA_SERVER_URL,
            json={
                "model": MODEL_NAME,
                "messages": [{"role": "user", "content": prompt}],
                "max_tokens": max_tokens,
                "temperature": temperature,
            },
            proxies=NO_PROXY,
            timeout=timeout,
        )
        resp.raise_for_status()
        return resp.json()["choices"][0]["message"]["content"].strip()


def status() -> list[dict]:
    _reclaim_stale_locks()
    entries = []
    if SLOTS_DIR.exists():
        for f in sorted(SLOTS_DIR.glob("slot_*.lock")):
            try:
                info = json.loads(f.read_text(encoding="utf-8"))
                info["age_s"] = round(time.time() - info.get("since", 0), 1)
                entries.append(info)
            except (OSError, json.JSONDecodeError):
                continue
    return entries


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--status", action="store_true")
    args = parser.parse_args()
    if args.status:
        entries = status()
        if not entries:
            print(f"All {MAX_SLOTS} slot(s) free.")
        else:
            print(f"{len(entries)}/{MAX_SLOTS} slot(s) in use:")
            for e in entries:
                print(f"  slot busy: client={e.get('client')} pid={e.get('pid')} "
                      f"age={e.get('age_s')}s job={e.get('job_id')}")
    else:
        parser.print_help()
        sys.exit(1)
