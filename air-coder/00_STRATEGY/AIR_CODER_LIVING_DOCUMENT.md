# AirCoder — Product Instruction / Living Document

Development target: 0.1.0-beta.3
Updated: 2026-09-08

## Purpose

AirCoder keeps the user's normal chat task entrypoint while selecting and adapting existing coding executors. It must prefer ready executor mechanisms over custom planning/agent infrastructure.

## Canonical boundaries

- product source: `air-coder/` inside the `air-plugins` repository;
- entry skill: `skills/route-coding-task/SKILL.md`;
- deterministic selector: `skills/route-coding-task/scripts/select_executor.py`;
- bounded ready-agent runner: `skills/route-coding-task/scripts/run_coding_task.py`;
- task contract: `contracts/coding-task.schema.json`;
- comparable result contract: `contracts/run-result.schema.json`;
- runtime receipts: `AIR_CODER_RUN_ROOT`, default `E:/-4-/air-coder/runs`;
- executor session/runtime remains owned by Codex, Claude Code or Ruflo.

## Primary ready-agent path

For the Astra adaptation cycle the first full working path is Codex CLI. Live evidence on SRVLM01 2026-09-08: `codex-cli 0.145.0`, authenticated with ChatGPT, and `codex exec --json` returns a persistent `thread_id`, completion events and token usage.
## Execution contract

1. Validate task/product/repository/context before allowing edits.
2. Run the selected ready executor with bounded scope; AirCoder does not ask it to commit, push or release.
3. Independently inspect changed paths and protected paths.
4. Run repository acceptance commands plus `git diff --check` outside the model.
5. On a product-check failure, resume the same Codex thread for at most two repair attempts.
6. On executor/infrastructure failure, stop without paying for an identical retry.
7. Persist status, `thread_id`, attempts, usage and evidence; an uncertain in-flight state is never silently replayed.

## Executor routes

- `chatgpt_rdc`: orchestration, analysis and small local engineering;
- `native_cli`: ready Codex/Claude repository agent; Codex is the primary AC-03 implementation path;
- `ruflo`: substantial/swarm route through `air-ruflo-bridge:run-via-ruflo`, using the documented dry-run → approval → execution sequence only.

## Non-goals

No custom planner, swarm engine, scheduler, watchdog, autonomous queue, learning database, model gateway or provider transport. AirCoder stores only task/receipt state needed to make execution verifiable and resumable.

## Current gates

The Codex runner remains `CANDIDATE` until one real isolated AC-03 task completes end-to-end. AC-04 then requires five comparable small tasks from at least two AIR products with the acceptance defined in the Astra 2026-09-08 specification. Release is a later gate; source implementation is committed/pushed first.