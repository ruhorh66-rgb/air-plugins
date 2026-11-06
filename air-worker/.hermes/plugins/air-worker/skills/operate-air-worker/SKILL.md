---
name: operate-air-worker
description: Use when a product is managed by AirWorker. Operate it only through the structured tool.
---
# Operate AirWorker
1. Call `air_worker` with `action: status` and the product root. Shadow status is observational; enforce status atomically runs orphaned pending work and judges completion before returning.
2. Treat `outcome: needs_action` as executable work, never as a terminal stop. If it is returned outside enforce mode, immediately call `action: run`; do not bypass the binary with terminal, edits, or parallel delegation.
   For the managed AirWorker product, `action: run` uses the **existing native `air-worker loop`**. Its canonical `run-config.json` now restores the accepted **script-first Codex coding ladder** (Luna → Sol, Astra only when justified), factual checks and independent Claude Sonnet/Opus judge. For two bounded coding tasks, the native **`air-worker executor dispatch -class complex` followed after terminal readback by `-class bulk`** preserves real workspace-write code edits and distinct receipts. `air-worker orchestrate -subagents 2` remains an optional mode for two read-only analysis agents plus one coding leader, not the default coding strategy. Do not use direct Codex/Claude commands, custom model routes or manual PLAN/LEARN edits. A historical R0.11.9-05 release guard remains a genuine HOLD, not permission to fall back.
3. A successful `run` must end as `completed`, `waiting`, or `running`. Treat `continuity_violation` as failure and do not claim completion.
4. Require `verified: true` before claiming completion. Call `action: verify` when it is absent.
5. Report closed/total, next action, stop reason, receipt paths, and log path.
6. Stop only on canonical PASS/COMPLETED, an LPR gate, or an evidenced blocker.

The binary owns plan state, transitions, retries, receipts, and verdicts.
