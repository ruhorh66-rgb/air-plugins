# llm-queue

A job queue for a local `llama-server`. Small, file-based, no service to run.

## What problem it solves

If several scripts call one local model server independently, they oversubscribe its
parallel slots and time each other out. A semaphore fixes that — and a semaphore is all
many setups have. It does not remember work, survive a restart, retry a failure or tell
you what happened.

This adds those four things on top of an existing semaphore rather than replacing it.
Two independent coordination mechanisms would defeat the purpose of having one.

```bash
dispatcher.py enqueue --kind review --input doc.txt --prompt-file p.txt
dispatcher.py enqueue-batch --kind review --dir ./cards --glob "*.md" --prompt-file p.txt
dispatcher.py run --limit 20
dispatcher.py status
dispatcher.py retry --failed
dispatcher.py show --job 17
```

## What is here

| File | Role |
|---|---|
| `llm_client.py` | slot semaphore — stops callers oversubscribing the server's parallel slots |
| `dispatcher.py` | the queue on top of it: priorities, restart survival, retry, visibility |
| `nightly_vault_fill.py` | unattended overnight run driven by a manifest |

All three keep their state and their site-specific configuration in the data directory,
never beside the code.

## Requirements

- a local OpenAI-compatible server (built against `llama-server`);
- an `llm_client.py` exposing `call_llm(prompt, client_name, ...)` — your slot semaphore;
- Python 3.10+ with `requests` (the interpreter your `llm_client` already uses).

`dispatcher.py` looks for `llm_client.py` in the data directory first, then beside
itself.

## Code here, state elsewhere

```bash
export LLM_QUEUE_DATA_DIR=/path/to/private/runtime
```

The queue database, the results and `llm_client.py` live there; only the code lives in
this repository. That separation is deliberate: results contain whatever was processed,
and that is exactly what must not follow the code into a public repo.

Without the variable, everything sits beside the script — fine for a scratch setup.

`nightly_vault_fill.py` additionally reads `nightly_config.json` from the data directory:

```json
{
  "air_vault_skill_scripts": "/path/to/skill/scripts",
  "python": "/path/to/python-with-requests",
  "llama_health_url": "http://127.0.0.1:8080/health",
  "llama_start_bat": "/path/to/start-server script"
}
```

Every value may also come from an environment variable instead. **Nothing has a
hard-coded default naming a real directory** — a default pointing at the wrong place
fails later and more confusingly than a missing-setting error at startup, and a default
naming a real machine leaks its layout into this repository.

The manifest of projects to process (`nightly_scan_projects.json`) is likewise data:
it names vaults and source roots, and stays private.

## Behaviour worth knowing

**Priority then age.** Lowest priority number first, oldest first within a priority.

**A dead worker frees its job immediately.** Job claims record a PID; if the process is
gone the job returns to the queue on the next run. Waiting out a timeout would strand
work for an hour after a crash that took milliseconds.

**Failures land in the row, not in the console.** Missing dependencies, server timeouts
and bad inputs are recorded against the job with the error text, and the job retries
until `max_attempts`. A worker that dies silently is the failure mode this avoids.

**Batches skip what is already done.** `enqueue-batch` will not re-queue an input already
queued, running or done for the same kind — pass `--allow-duplicates` if you want a
second opinion. Silent re-runs cost model time and produce results indistinguishable
from independent ones.

**Truncation is loud.** Input longer than `input_char_limit` is cut with a visible marker
rather than quietly, because a silently truncated input yields a confident answer about
material the model never saw.

## Status

Written 2026-07-20 and exercised on a real sweep of 160+ jobs across two task types. The
three behaviours described above each exist because the first run got them wrong.
