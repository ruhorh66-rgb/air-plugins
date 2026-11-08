---
name: operate-air-worker
description: Use when a product is managed by AirWorker. Operate it only through the structured tool.
---
# Operate AirWorker
1. Call `air_worker` with `action: status` and the product root. Shadow status is observational; enforce status atomically runs orphaned pending work and judges completion before returning.
2. Treat `outcome: needs_action` as executable work, never as a terminal stop. If it is returned outside enforce mode, immediately call `action: run`; do not bypass the binary with terminal, edits, or parallel delegation.
   For the managed AirWorker product, `action: run` uses the **existing native `air-worker loop`**, with **mandatory two Codex coding subagents** declared in canonical `run-config.json` (`orchestration.enabled=true`, `subagents=2`, `require_native_subagents=true`). Each of TWO Codex workers reads the TASK and writes actual source files in its OWN separate Git worktree with workspace-write sandbox. The ChatGPT conversation, not an additional Codex model, is the coordinator: it reads both native job receipts and actual Git diffs, performs explicit integration on the shared source, then factual QA and independent Claude Sonnet/Opus judge. No third Codex leader is permitted. Narrative-only or text-diff-only output without physical files is NOT a coding worker. Route models using the approved OpenAI ladder Luna 6 → Terra 6.1 → Sol 6 → Sol 6.1 → Astra 6, and escalate only for an evidenced reason; do not force all tiers. Use `air-worker orchestrate code -product <root> -task-file <TASK> -principal gpt -session-key <chat>` when controlled from ChatGPT, or the canonical `air_worker(action=run)` for Hermes native operation. The coordinator must inspect/merge before any factual QA and independent judge; never two sequential `executor dispatch` calls as a substitute. Do not use direct Codex/Claude commands, custom model routes or manual PLAN/LEARN edits. A historical R0.11.9-05 release guard remains a genuine HOLD, not permission to fall back.
3. A successful `run` must end as `completed`, `waiting`, or `running`. Treat `continuity_violation` as failure and do not claim completion.
4. Require `verified: true` before claiming completion. Call `action: verify` when it is absent.
5. Report closed/total, next action, stop reason, receipt paths, and log path.
6. Stop only on canonical PASS/COMPLETED, an LPR gate, or an evidenced blocker.

The binary owns plan state, transitions, retries, receipts, and verdicts.
