---
name: operate-air-worker
description: Use when a product is managed by AirWorker. Operate it only through the structured tool.
---
# Operate AirWorker
1. Call `air_worker` with `action: status` and the product root.
2. If work is pending, call `action: run`; do not bypass the binary with terminal, edits, or parallel delegation.
3. Call `action: verify` before claiming completion.
4. Report closed/total, next action, stop reason, receipt paths, and log path.
5. Stop only on canonical PASS/COMPLETED, an LPR gate, or an evidenced blocker.

The binary owns plan state, transitions, retries, receipts, and verdicts.
