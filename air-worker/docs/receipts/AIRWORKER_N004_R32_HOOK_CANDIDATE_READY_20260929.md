# AirWorker N-004 — refreshed AIR Commander r3.2 hook candidate — 2026-09-29

Node: SRVLM01
AirWorker parent: N-004_0-11-2-chatgpt-host-hook-transport-gap
Replacement node: N-024_n-004-refresh-chatgpt-hook-cutover-on-air-comman
Superseded node: N-013_n-004-controlled-live-hook-cutover-gate

## Why N-013 is superseded

N-013 referenced the old AIR Commander candidate at cbc14b9.

AIR Commander r3.1 / bridge 0.1.1 is now live and contains the accepted completed-process-output retention fix. Deploying cbc14b9 would regress that live fix. Therefore cbc14b9 is no longer a valid cutover candidate.

## Replacement candidate

Repository:
https://github.com/ruhorh66-rgb/air-commander

Branch:
fix/r3.2-airworker-hooks-20260929

Base:
- r3.1-2026-09-29
- commit 3107353

Source implementation:
- 8b6a3cc feat: layer ChatGPT AirWorker lifecycle on AIR Commander r3.1

Acceptance closure:
- 9750e29 plan: accept AIR Commander r3.2 AirWorker hook candidate

Bridge candidate version:
0.1.2

AIR Commander receipt:
docs/receipts/AIR_COMMANDER_R32_AIRWORKER_HOOK_SOURCE_ACCEPTANCE_20260929.md

## Preserved r3.1 behavior

The candidate retains:
- ProcessOutputArchive
- upstream completed-session eviction recovery
- PID reuse protection
- bounded process-output archive
- all r3.1 bridge behavior

## Added ChatGPT → AirWorker behavior

- air_worker_attach
- air_worker_status
- air_worker_pending
- air_worker_finalize
- private/app-only air_worker_approve
- principal=chatgpt session declaration
- SessionStart
- automatic PreToolUse / PostToolUse for ordinary AIR Commander calls
- Stop lifecycle
- trusted UserPromptSubmit approval path
- minimal transcript without tool arguments
- approval widget resource

## Verification

Targeted:
- 15 PASS
- 0 FAIL

Full AIR Commander bridge:
- 99 total
- 98 PASS
- 0 FAIL
- 1 Windows symlink fixture SKIP (EPERM)

git diff --check:
PASS

## Rollback / live boundary

Current live rollback target remains AIR Commander r3.1 / bridge 0.1.1.

No r3.2 install/restart was performed.

N-004 remains OPEN until:
1. the current AirWorker release is live,
2. r3.2 is installed through the approved release path,
3. ASW restart is separately approved,
4. ChatGPT Refresh/new-session exposes the AirWorker tools,
5. live attach / PreToolUse / PostToolUse / Stop / private approval acceptance passes.

## Result

N-024: SOURCE READY — close.
N-013: SUPERSEDED BY N-024 — close.
N-004: remains OPEN as the single live transport acceptance gate.
