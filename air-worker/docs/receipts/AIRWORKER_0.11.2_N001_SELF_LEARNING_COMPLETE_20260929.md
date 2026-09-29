# AirWorker 0.11.2 N-001 self-learning acceptance — 2026-09-29

Scope: L11-1..L11-6 implementation completeness. This receipt accepts the mechanism; it does not grant or apply any still-PENDING_LPR proposal.

## L11-1 — canonical learning state
- canonical `learn/events.jsonl`, proposals registry, verified rules and private digest ledger are separated by role;
- legacy AirCurator journal is migration-only and direct writes are blocked by core policy;
- real AirCurator deltas were imported idempotently through `learn migrate-legacy` (latest isolated verification: first import 1, second import 0; source branch receipt commit `89a5f58`).

## L11-2 — judge freshness
- cached factual verdict has 6h maximum age and carries/checks git HEAD plus input fingerprint;
- report/drift reject stale verdicts instead of reporting a numeric distance;
- commit/tick paths refresh factual evidence through the binary-owned judge path.

## L11-3 — daily partner
- daily review runs immediately when no prior review exists, then no more often than every 24h;
- input packet includes canonical events from the last 24h, active rules, event classes and transcript evidence;
- GPT Stop without transcript evidence does not spawn a review; AIR Commander integration supplies a minimal host trace.

## L11-4 — executable learned rules
- approval transaction is generic over registered `hook|gate|script` check specifications;
- verifier/testcase receipt is mandatory before activation;
- active hook dispatch is registry-owned and fails closed if implementation is missing;
- apply/rollback transitions are visible in canonical `learn/events.jsonl` lifecycle events.

## L11-5 — curator tick
- patrol coverage remains fail-closed;
- tick v3 includes course progress, >1h stalls, idle windows, pending gates and deterministic actions;
- factual judge freshness is refreshed by tick path.

## L11-6 — Hermes-parity lifecycle
- per-rule usage and `last_activity` telemetry;
- 14d -> STALE, 30d -> ARCHIVED without deleting rule history;
- weekly maintenance runs on first curator tick and then no more often than every 7d, with snapshot, REPORT.md, run.json and proposal-only merge review;
- active learned rules are exposed as compact indexed skills (description <=60 runes) and full body loads on demand by id/class with When to apply / Procedure / Pitfalls / Verification sections.

## Acceptance
- all child plan nodes N-005..N-011 are closed with their individual receipts;
- final `go test . -count=1 -timeout 120s`: PASS, 120.319s;
- final `go vet ./...`: PASS;
- branch before closure: `dev/airworker-0.11.2-gpt-learn-lifecycle` at `8136294` plus N-011 closure commit `9d1bf61` and N-010 closure commit `8136294`.

## Explicit non-claim
AirCurator proposal `LP-20260928T133444Z-31d4ca9c` remains PENDING_LPR until the user supplies the exact approval through a trusted UserPromptSubmit/approval UI. No grant is fabricated by this acceptance.
