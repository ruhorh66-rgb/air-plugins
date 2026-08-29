# air-worker

`air-worker` is the canonical dual-host plugin for Claude and Codex. It executes
heavy non-swarm tasks through the existing `llm-queue`.
Version 0.3.0 changes the default: a signed request is created and executed locally
immediately. Telegram approval remains available only as `--approval telegram`.

Install and release this directory as the one Claude+Codex package. The sibling
`../air-worker-codex` directory is a checkout-only compatibility adapter for older
Codex layouts; it runs this shared core and is not a release source. It contains no
duplicate executor, runtime, or secret.

## Direct execution

```text
python skills/run-worker-task/scripts/worker.py create openrouter-llm <file> \
  --param model=nvidia/nemotron-3.5-lightning:free \
  --param instruction="..." --privacy external
```

Set `AIR_WORKER_RUNTIME` to a host-local runtime directory when you need an explicit
location. Otherwise the resolver uses `AIR_RUNTIME_ROOT` when present, then the
platform state directory (`%LOCALAPPDATA%/air-worker` or `$XDG_STATE_HOME/air-worker`).
An existing legacy `E:\-4-\air-worker` state is read-compatible migration fallback only.
On hosts without the existing Windows keyring setup, supply `AIR_WORKER_HMAC_KEY`
through the host secret store. `AIR_WORKER_FREE_ONLY=1` is the default and only
admits `:free` OpenRouter models. `--privacy local` rejects external OpenRouter
execution.

The contract is HMAC-signed and includes the task type, parameters, input path and
input digest. Before execution the core rechecks the signature, registry, privacy,
input size and executor bounds. It uses a single atomic claim:

```text
queued → running → done | failed | invalid
```

An id cannot execute twice. A failed/interrupted task is recovered with a new signed
request; llm-queue owns bounded retries and concurrency, so this plugin does not
start a second competing process.

Direct queue dispatch now requires the queue's targeted `run-job` and
`show-job-json` capability contract. It atomically starts exactly one known queue
id, then polls that same id's redacted JSON receipt. The historical global
`run --limit` implementation is unsafe with concurrent jobs and is rejected
fail-closed; see [docs/GOAL.md](docs/GOAL.md).

## Router dependency

The OpenRouter executor requires an OpenAI-compatible router at
`AIR_WORKER_ROUTER_URL` (default
`http://127.0.0.1:8090/v1/chat/completions`). Health checks use
`AIR_WORKER_ROUTER_MODELS_URL` (default `http://127.0.0.1:8090/v1/models`).
The router is an external host service: this cross-platform plugin does not read its
OpenRouter key, copy secrets, or silently start a privileged process. The current
Windows deployment supervises it with a user-level scheduled task; POSIX and other
hosts may use their native user service manager while keeping the same two URLs.

## Product vault

On the current AIR OS coordinator, the canonical product-owned vault is
`E:\-5-\011_Plugins\AirWorker_Wiki`. It contains roadmap, product tasks,
operations/runbooks, release history and accepted product knowledge. It is not a
runtime directory: requests, results, metrics, protocol and secrets remain under
the host-local `AIR_WORKER_RUNTIME`. `010_Task_Control_Platform` keeps only the
aggregated status and a link. Other hosts resolve the equivalent coordinate through
their AIR Storage registry instead of assuming the Windows drive path.

## Legacy Telegram

Use `--approval telegram` only when a human button is wanted. Then run
`listener.py --once 30`. The listener preserves shared-bot locking, callback-only
input and chat-id validation, but delegates execution to the same core.

## Observability and safety

`protocol.py` is append-only JSONL with separate external legs; `run_metrics.csv`
records model/counters/outcome. Prompt payload, secrets, and local input/output paths
are excluded from protocol and metrics. Results remain in the configured local
runtime, identified by task id and status.

Run `python skills/run-worker-task/scripts/selftest.py` from this plugin root for the
offline suite. It includes 36 checks; live route verification is separate. See
[docs/GOAL.md](docs/GOAL.md) and the shared skill for migration and acceptance.
