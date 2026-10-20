# air-worker

## 0.11.9 candidate — machine-verifiable shared learning (source accepted; release gated)

The AirWorker executable and dual-host Claude/Codex plugin consume the published `air-modules/learning v0.1.0`, without duplicating its writer. The native CLI records a signed failure (`run_id` and source), applies only an independently reviewed safe Markdown procedure, then credits it **only after** exact-SHA `skill_loaded`, a real later `procedure_used` and independently verified outcome/effect. In the accepted example, missing `run-config.json` causes **deferred validation** and prevents one redundant retry; this **is not a validation PASS**. A proposal, loaded file or signed intent without a final result is not successful use.

Operational commands are `air-worker version`, `selfcheck -json`, `plan spine -product <root> -json`, `plan node list -product <root> -open -json`, `plan-lint -product <root> -json`, `validate -product <root> -json` and (only for a configured self-learning owner) `learn self status -json`, `learn status`, `learn load` and `loop -product <root>`. `loop` R02–R04 is gated by the exact 0.11.9 candidate, configured self-learning owner and signed native runtime receipts. Do not treat an isolated E2E as a change of the main product's release-slice status.

Current status as of 20.10.2026: frozen Go source `12eae0104b07b496951f40353cd44f190ba29f6b` passed full QA and isolated real R02–R04; the **primary product has now also closed R02, R03 and R04 using the native 0.11.9 core** and exact signed capture/review/use receipts. R03 performed exactly one genuine model review and one safe APPLIED procedure; R04 confirmed one later module-owned `procedure_used` with signed intent/result and `effect=avoided_repeat_validation_with_missing_config`. This is a deferred validation, **not a full validation PASS**. `N-070`/`N-093` CLOSED, `N-071` OPEN; primary release-slices R05 OPEN and R06–R07 PLANNED. The current native loop implements R02–R04, not R05–R07. Release 0.11.9 is **NOT tagged, published or installed**; installed 0.11.8 is unchanged. N124 and DEV136 remain deferred. Portable release assets must match the final Git source SHA in both Windows CLI/tray and package SHA-256; Authenticode was waived for this version only. Publication and installation require separate exact LPR permissions; rollback target is installed 0.11.8.

> **Start here (0.10.x).** The product is the `air-worker` binary: judge, goal-drift engine,
> loop, planner, report, feedback, install/tray. Hooks only deliver host events to it; skills
> are optional help. A new session starts from the product instruction
> (`E:\-5-\011_Plugins\AirWorker_Wiki\PRODUCT_INSTRUCTION.md` on the AIR OS coordinator) and
> `PLAN.md`, section «Состояние и порядок старта». The released version is the newest
> `air-worker--v*` tag on `origin/main`; the installed one is what `air-worker install -status`
> reports. Branches `release/*`, `fix/*`, `wip/*` that are not merged into `main` are not releases.
>
> The sections below describe the heavy-task executor `skills/run-worker-task` (signed requests
> through `llm-queue`, default since 0.3.0). It is one executor inside the product, not the 0.10.x
> control plane.

`air-worker` is the canonical dual-host plugin for Claude and Codex. It executes
heavy non-swarm tasks through the existing `llm-queue`.
Since 0.3.0 its default is direct execution: a signed request is created and executed
locally immediately. Telegram approval remains available only as `--approval telegram`.

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
fail-closed; see the «Current release gate» section of
[skills/run-worker-task/SKILL.md](skills/run-worker-task/SKILL.md).

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
their AIR Storage registry instead of assuming the Windows drive path. The session entry
point there is `PRODUCT_INSTRUCTION.md`; the canonical and only development plan is
`PLAN.md` in this directory.

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
[skills/run-worker-task/SKILL.md](skills/run-worker-task/SKILL.md) for migration and acceptance.
