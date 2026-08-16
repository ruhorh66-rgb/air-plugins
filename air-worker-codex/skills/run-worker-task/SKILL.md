---
name: run-worker-task
description: Codex adapter for the shared air-worker core. Create a signed local task and execute it directly through llm-queue; use Telegram only with explicit --approval telegram.
---

# air-worker for Codex

This is a thin host adapter, not a second runtime. Invoke
`air-worker-codex/scripts/run_worker.py`; it resolves and runs the shared Python core
at `../air-worker/skills/run-worker-task/scripts/worker.py` from the repository root.
It is the same signed contract, executor registry, llm-queue dispatch, status database,
metrics, and protocol used by the Claude plugin.

Default invocation is direct:

```text
python air-worker-codex/scripts/run_worker.py create openrouter-llm <file> \
  --param model=nvidia/nemotron-3.5-lightning:free --param instruction="..." --privacy external
```

Set `AIR_WORKER_RUNTIME` for the host runtime and, where no keyring integration exists,
provide `AIR_WORKER_HMAC_KEY` via the host secret store. Never put either secret, the
prompt, or source material in task messages/logs. Use `--approval telegram` only for
the optional legacy button workflow; it is never required for direct execution.

Read the shared [contract and cycle](../../../air-worker/skills/run-worker-task/SKILL.md)
before invoking the core. Run its offline `selftest.py`; live verification is separate.
