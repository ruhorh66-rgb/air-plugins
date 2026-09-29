# AirWorker N-004 GPT host hooks — live cutover ready — 2026-09-29

Status: GATE_READY / LIVE CUTOVER NOT EXECUTED

## Current live SRVLM01
- AirWorker live: 0.11.2 (accepted release; N-001/N-002/N-003 closed).
- ASW installed: 0.7.1.
- ASW lifecycle: `profile=devspace-bridge state=RUNNING pid=10316 instance=pid:10316 desired=running`.
- bridge PID 10316 is child of interactive ASW; lock PID and listener 127.0.0.1:8730 match; health marker RUNNING/OK and fresh.
- live AIR Commander bridge source code tree: `F:\-8-\air-commander`, HEAD `ae1b9a62175a0862fa29e17e3618cd6d7bb9a554`; no tracked `bridge/` modifications.
- runtime config SHA-256: `8967F1A9720066F83DE4E11935210317A4DAAEE5B57932428269AEF2A22108E4`; accessMode remains `full` and is out of scope for this hooks-only cutover.
- ASW user overlay: `C:\ProgramData\AIR OS\State\startup.user.rules`, SHA-256 `712E631E5E56AD2B22B1861C9B863CE0E7C96E6628BD4518EBB03CBEDC737A1D`; main working_directory is `F:\-8-\air-commander`.

## Candidate
- clean detached worktree: `F:\-8-\_worktrees\air-commander-gpt-hooks-live-candidate-20260929`.
- exact code commit: `cbc14b9bf8030e2488050b5df01b1e3ea5f49310`.
- accepted middleware: signed ChatGPT session attach, automatic PreToolUse/PostToolUse, explicit Stop finalizer, private single-use approval UI/token, minimal transcript without tool arguments.
- full bridge regression on clean detached worktree: 95 tests / 94 PASS / 0 FAIL / 1 Windows symlink fixture SKIP (EPERM).
- source receipt: AIR Commander `docs/receipts/AIRCOMMANDER_GPT_AIRWORKER_SOURCE_ACCEPTANCE_20260928.md`.
- controlled-cutover package: AIR Commander `docs/receipts/AIRCOMMANDER_GPT_AIRWORKER_CUTOVER_PACKAGE_20260929.md`.

## Exact gated cutover
No reboot and no UAC are expected. Do not execute until explicit LPR live-cutover approval.

1. Re-read and hash `startup.user.rules` and `bridge.config.json`; abort if either differs from the hashes above.
2. Back up `startup.user.rules` byte-for-byte.
3. Atomically change only:
   `devspace-bridge.working_directory = F:\-8-\air-commander`
   to
   `devspace-bridge.working_directory = F:\-8-\_worktrees\air-commander-gpt-hooks-live-candidate-20260929`.
   Leave Test profile, keys, relay, access mode, roots and all other rules unchanged.
4. Run only the ASW-owned lifecycle command:
   `C:\Program Files\AIR\ASW\asw.exe air-commander restart`.
   No manual kill/start and no second supervisor.
5. Require a new PID and verify: desired=running; exactly one bridge; PID==lock PID==port 8730 owner; health marker PID/start matches and becomes RUNNING/OK with fresh relay success.
6. Refresh AIR Commander in ChatGPT Plugin settings and open a new chat/session because tools/list is snapshotted by ChatGPT.
7. Verify visible public tools: `air_worker_attach`, `air_worker_status`, `air_worker_pending`, `air_worker_finalize`; `air_worker_approve` remains app/private-only.
8. Live disposable acceptance: attach -> SessionStart -> harmless AIR Commander tool -> PreToolUse/PostToolUse -> finalize/Stop; then pending proposal -> private approval token/UI -> UserPromptSubmit grant -> apply. Verify AIR Commander audit and AirWorker traces share the same ChatGPT session.

## Rollback
On any failure, restore the backed-up `startup.user.rules` (working_directory `F:\-8-\air-commander`) atomically and run the same ASW-owned `air-commander restart`. Do not change AirWorker 0.11.2, keys, runtime config or access mode. Refresh ChatGPT again and leave N-004/N-013 open with failed acceptance evidence.

## Gate
This receipt is preparation only. AIR Commander cutover package explicitly states `LIVE CUTOVER GATED`; no live source switch or restart has been executed here.
