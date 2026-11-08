---
name: operate-air-worker
description: Operate AirWorker only through its native structured tool.
---
# Operate AirWorker

1. Call `air_worker` with `action: status` and product root. Shadow is observational; enforce may recover owned work and judge.
2. On `outcome: needs_action`, call `action: run` in enforce. The native `air-worker loop` owns state, execution and receipts. Do not bypass it with shell or direct providers.
3. Managed coding uses `air-worker orchestrate code`: exactly two Codex code writers in separate Git worktrees (`workspace-write`). Submit two distinct, small, schema-checked microsteps (`file`, `instructions`, `done_when`); missing or overlapping scopes must fail. ChatGPT coordinator verifies two real diffs and receipt SHA, then integrates. There is no third Codex leader.
4. After integration, require factual Go QA and independent Claude source/security review at the frozen SHA. Model `DONE` or handoff `READY_FOR_CHATGPT_COORDINATOR_INTEGRATION` is not product PASS.
5. Use `action: verify` if `verified: true` is absent. Accept only `completed`, `waiting`, `running`; `continuity_violation` is failure. Report closed/total, next action, stop reason, receipt and log.
6. Escalate tiers only for an evidenced failure/stall. Never use direct Codex/Claude, PAYG/custom routes, manual PLAN/LEARN edits, unauthorised release/install or foreign cleanup. Stop only for measured PASS, LPR gate or evidenced blocker.
