# air-worker goal

plan_for_version: 0.3.0

Supersedes the previous version of this file.

## Goal

Make signed local execution the default for both Claude Code and Codex while retaining
the Telegram approval path solely as an explicit legacy compatibility mode.

## In scope

- one host-neutral Python execution core shared by direct create and listener;
- signed task contract, registry, privacy classification, `:free`, input/timeout bounds,
  llm-queue concurrency/retry ownership, protocol and metrics;
- direct state transition and idempotent claim/recovery behavior;
- thin Codex adapter with no copied runtime;
- offline and one safe live direct OpenRouter acceptance.

## Boundaries

- no Telegram call, listener state, or shared bot lock is prerequisite to direct execution;
- legacy Telegram retains callback/chat validation and locks;
- no change to llm-queue, router billing guard, running foreign listener, marketplace/cache,
  installed plugins, or secrets;
- protocol and metrics exclude prompt payloads, secrets, and local input/output paths.

## Acceptance

- direct work succeeds while the foreign listener is held and makes zero Telegram calls;
- invalid/tampered contracts, non-free models in free-only mode, local material sent to
  OpenRouter, malformed router responses, and HTTP errors never reach `done`;
- legacy listener works offline with the shared core; status/result/metrics/protocol are auditable;
- existing selftests plus the new direct tests pass, packaging validates, and the live
  `nvidia/nemotron-3.5-lightning:free` task succeeds without touching PID 16044.

## Deferred

Installation, marketplace/cache changes, tag/push/merge, and changing `defaultEnabled`
require their own explicit release decision after live acceptance.

## Queue capability and deferred controls

Direct execution requires the queue to advertise JSON capabilities `run-job` and
`show-job-json`, and uses only targeted `run --job` plus `show --job --json`. The
feature worktree supplies that contract; an older installed queue still fails closed
rather than using global `run --limit`. This repository does not access the queue
database. Retry/backoff policy, cancellation, durable resume, and 429 handling remain
explicitly deferred; no support for those controls is claimed here.
