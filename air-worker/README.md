# air-worker

`air-worker` executes heavy non-swarm tasks through the existing `llm-queue`.
Version 0.3.0 changes the default: a signed request is created and executed locally
immediately. Telegram approval remains available only as `--approval telegram`.

The runtime code is shared by the Claude plugin and the thin sibling Codex adapter
(`../air-worker-codex`); the adapter contains no duplicate executor or secret.

## Direct execution

```text
python skills/run-worker-task/scripts/worker.py create openrouter-llm <file> \
  --param model=nvidia/nemotron-3.5-lightning:free \
  --param instruction="..." --privacy external
```

Set `AIR_WORKER_RUNTIME` to a host-local runtime directory. On hosts without the
existing Windows keyring setup, supply `AIR_WORKER_HMAC_KEY` through the host secret
store. `AIR_WORKER_FREE_ONLY=1` is the default and only admits `:free` OpenRouter
models. `--privacy local` rejects external OpenRouter execution.

The contract is HMAC-signed and includes the task type, parameters, input path and
input digest. Before execution the core rechecks the signature, registry, privacy,
input size and executor bounds. It uses a single atomic claim:

```text
queued → running → done | failed | invalid
```

An id cannot execute twice. A failed/interrupted task is recovered with a new signed
request; llm-queue owns bounded retries and concurrency, so this plugin does not
start a second competing process.

Direct queue dispatch now requires the queue's targeted `run-job`, `wait-job`, and
`cancel-job` capability contract. The current global `run --limit` implementation is
unsafe with concurrent jobs and is rejected fail-closed; see [docs/GOAL.md](docs/GOAL.md).

## Legacy Telegram

Use `--approval telegram` only when a human button is wanted. Then run
`listener.py --once 30`. The listener preserves shared-bot locking, callback-only
input and chat-id validation, but delegates execution to the same core.

## Observability and safety

`protocol.py` is append-only JSONL with separate external legs; `run_metrics.csv`
records model/counters/outcome. Prompt payload, secrets, and local input/output paths
are excluded from protocol and metrics. Results remain in the configured local
runtime, identified by task id and status.

Run `python skills/run-worker-task/scripts/selftest.py` for the offline suite.
It includes 33 checks; live route verification is separate. See
[docs/GOAL.md](docs/GOAL.md) and the shared skill for migration and acceptance.
