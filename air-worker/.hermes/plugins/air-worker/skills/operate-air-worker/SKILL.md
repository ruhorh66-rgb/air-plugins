---
name: operate-air-worker
description: Use when a product is managed by AirWorker. Operate it only through the structured tool.
---
# Operate AirWorker
1. Call `air_worker` with `action: status` and the product root. Shadow status is observational; enforce status atomically runs orphaned pending work and judges completion before returning.
2. Treat `outcome: needs_action` as executable work, never as a terminal stop. If it is returned outside enforce mode, immediately call `action: run`; do not bypass the binary with terminal, edits, or parallel delegation.
3. A successful `run` must end as `completed`, `waiting`, or `running`. Treat `continuity_violation` as failure and do not claim completion.
4. Require `verified: true` before claiming completion. Call `action: verify` when it is absent.
5. Report closed/total, next action, stop reason, receipt paths, and log path.
6. Stop only on canonical PASS/COMPLETED, an LPR gate, or an evidenced blocker.

The binary owns plan state, transitions, retries, receipts, and verdicts.
