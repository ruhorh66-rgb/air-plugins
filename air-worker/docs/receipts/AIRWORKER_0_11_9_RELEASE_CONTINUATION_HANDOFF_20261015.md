# AirWorker 0.11.9 — release continuation handoff

**Date:** 2026-10-15 SGT. **Branch:** `dev/airworker-0.11.9-selflearning-only-20261011`. **Parent before checkpoint:** `038acf9f63b52a3d7cc077e773ec470f068d0f64`. This handoff is a development checkpoint, not a release receipt, acceptance, or gate closure.

## Checkpoint scope

The checkpoint preserves the branch's existing 0.11.9 WIP, including the current release-slice status/loop work, adapter status, semantic review, model-policy, plan/goal updates, manifests, tests, and the tracked local candidate binary. No pre-existing dirty file was discarded. The commit SHA is the pushed branch tip containing this handoff; read it with `git rev-parse HEAD`.

Previously verified for this source set: `go test . -count=1 -timeout 8m` from `air-worker/cmd/` passed (`ok airos/air-worker 239.785s`); `go vet .` passed; `git diff --check` was clean. The local candidate was last built as AirWorker 0.11.9, SHA-256 `30e93a269daba8edd122831a6c3caf53c5d67be4a79b987e37c5ebd82d6bbe34`. Reverify its version and hash before using it for any future gate; it is not a published artifact.

## Live release/install facts

- The official GitHub latest release is `air-worker--v0.11.8`; active Claude and Codex plugin registrations/caches report 0.11.8. Installed CLI SHA-256: `1b4008af05110214934d46c0ab8c5236341cd1c699f54efb895c2f9f64a94a94`.
- Structured `air_worker status` invoked `C:\Users\admin_loc\AppData\Local\air-worker\bin\air-worker.exe` (0.11.8) against this product and returned `current_step: 93`, `next_action: run_loop`, `stop_reason: no_live_worker`. Do not run that stale binary on this 0.11.9 plan; it would target the numeric PLAN step instead of the active release slice.
- The current branch's `plan/release-slices.json` keeps `R0.11.9-02` open; later 0.11.9 slices remain planned. R02 requires current-candidate native capture, signed receipt, and exact replay/readback. The implementation must be invoked only through a supported AirWorker-native route; do not substitute direct CLI calls, binary/config overrides, or manual plan edits.
- A similarly named R02 receipt exists only in `dev/airworker-0119-real-capture-20261012`, bound to source HEAD `bbfa050c4788b7c8d7960975825205e9e5cee3f3` and binary SHA-256 `cf16c2cd4b32a6c8ae3d3bc9504fb24cb3059e018b89884ff1e726b3faf3194c`. Its status is `SCOPED_NATIVE_CAPTURE_PASS_INDEPENDENT_JUDGE_PENDING`; it is not evidence for this branch/candidate and must not be reused or treated as independent review PASS.
- Pre-freeze gates remain open: N-070 is `NOT_PROVEN` (published `air-modules/learning 0.1.0` tag/SHA plus consumer integration regression); N-071 is `OPEN` and must be reconciled only through native AirWorker with node/index/receipt readback. R0.11.9-07 forbids tag/release while these are open.
- The native installer is available as `air-worker install`, but the non-elevated path accepts only the canonical GitHub marketplace cache. No 0.11.9 tag/release or canonical cache package exists, so installation must wait for a gate-complete official artifact. The installed 0.11.8 remains untouched. `install -status` also reported a Codex profile ambiguity and stale profile-cache violations; do not change host profiles/config as part of this checkpoint.

## Next safe sequence

1. Start from this pushed branch tip; read back branch/HEAD and current dirty state before further edits. Preserve the existing WIP and all receipt/runtime state.
2. Resolve the supported native-tool/candidate routing without bypasses, then complete R0.11.9-02 on this exact candidate. Verify receipt, capture and replay readback before changing slice status.
3. Complete R0.11.9-03/04 with their own exact-candidate evidence; do not claim next-session use without a real fresh-process `procedure_used` and outcome/effect.
4. Close N-070/N-071 by their canonical processes and read back node, index, receipt, pinned module SHA, and integration regression. No manual node/receipt edits.
5. Freeze exact source/plan SHA; run full Go tests, vet, build, security checks, smoke and rollback dry-run; obtain independent read-only Codex `gpt-6-sol:medium` review on that exact SHA and factual acceptance receipts.
6. Only after every release gate passes, use the authorized release path: create/verify the exact GitHub tag/release and marketplace package, then install that artifact and read back installed version, SHA, install status, active host profiles, and release-slice status. Do not tag/publish early or alter the old installation before the official package is ready.

**Boundary:** this checkpoint commit/push does not close any release slice or N-node, does not publish a tag/release, does not update marketplace caches, and does not install 0.11.9.