# Dev Work 26 / native Codex subscription handoff
Owner: Dev Work 26 (SRVLM01)
Trigger: LPR 2026-11-04 requires existing AirWorker native Codex executors + Anthropic reviewer, then exactly two parallel Codex subagents for Hermes-parity P0.
Done when: this read-only native executor demonstrates it actually read the source and returns an exact concrete CLI route to run two parallel native agents, without using codex app-server; this is ONLY executor smoke, not orchestration acceptance.
Judge: native AirWorker executor job receipt (process_started/turn.completed/exit) and an answer quoting exact source function names; return not_proven if a file cannot be read.
Receipt: native job record created by AirWorker.

Read only these exact product-owned files:
- cmd/runner_codex.go: invokeCodexOrchestrated
- cmd/orchestration.go: cmdOrchestrate
- cmd/loop.go: cmdLoopWithReleaseSliceRunner
- cmd/release_slice_runner.go: releaseSliceLoopSupported
- cmd/semantic.go: oppositeSemanticReviewer

Return a concise source-verified report:
1. how the two *parallel* Codex subagents, leader and independent Claude reviewer are wired by the existing AirWorker code;
2. one narrow next safe native action to progress Hermes P0, accounting for the historical R0.11.9-05 release guard and the currently unavailable Claude subscription auth in the active Windows profile;
3. model/effort actually used by this executor if observable.
Do NOT edit or create files, run QA/vet, install, switch provider, invoke paid API, create alternate router, delete sidecar, fabricate approvals, or claim that this single task proves orchestration.
