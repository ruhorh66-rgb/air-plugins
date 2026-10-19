# AirWorker 0.11.9 — Worker B R04 handoff (2026-10-20 SGT)

**Block status:** R04 SOURCE WIP SAVED / HANDOFF; focused QA PASS; final independent R04 acceptance, full current-source regression, real production E2E and release approval are still OPEN. This is NOT an accepted release and NOT an instruction to install.

## Where to continue

- AIR node: `SRVLM01`. Use AIR Commander / AIR Commander Test with explicit `node: SRVLM01`, never enter SRVLM01 by a separate shell or SSH.
- Repository root: `R:\-4-\air-worker\worktrees\airworker-0119-n093-worker-b-20261018`.
- Go product root: `R:\-4-\air-worker\worktrees\airworker-0119-n093-worker-b-20261018\air-worker`.
- Source branch: `dev/airworker-0119-n093-worker-b-20261018`. Official remote: `https://github.com/ruhorh66-rgb/air-plugins.git`; query `git rev-parse HEAD` and `git ls-remote github refs/heads/dev/airworker-0119-n093-worker-b-20261018` to find the exact handoff commit. Do not assume the base SHA below is the final handoff commit.
- Baseline before R04 WIP: `e51b64f86eaea9132fd088193a177b8c5c5adce6` (native N-093 CLOSED). All 15 source/doc WIP paths are frozen and enumerated by SHA in the external machine QA request.
- Pinned dependency: the *published* `github.com/ruhorh66-rgb/air-modules v0.1.0`. No module v0.1.1 or new API is needed. Installed AirWorker 0.11.8 remains untouched.

## PLAN vs verified fact

- Native `plan spine`, `plan node list`, `plan-lint`, `validate`: all RC0 (read-only). The exact log hashes are in the external `PLAN_READBACK.json`.
- Native N-093 is `closed` with the original B4 independent Astra high real procedure-used receipt. The close was committed by the one designated native PLAN writer.
- Actual canonical release-slices: R0.11.9-02 `open`, R03 `planned`, R04 `planned`, R05–R07 `planned`. None has been closed here.
- Previous R02/R03 **isolated** source and production evidence passed; historical R03 APPLIED is NOT an actual R04 use.

## Implemented R04 in the handoff source

- Native R02 outer signed release envelope now also authenticates the exact original disposable scratch target (`target_root`). Its original CLI invocation HMAC must match.
- R04 supports `test` stage and only the exact R04 OPEN → CLOSED / R05 PLANNED → OPEN byte/CAS plan transition; older R03 tests were corrected to test genuine fail-closed rejection instead of blanket unsupported status.
- R04 checks current reviewer/APPLIED/index/target@SHA, valid R02 and R03 history, one actual later module-owned `skill_loaded` and `procedure_used`, separate HMAC-signed N093 intent and terminal result. Outcome is `deferred_missing_configuration`; effect is `avoided_repeat_validation_with_missing_config`. Full validation did NOT run.
- A source-owned HMAC R04-OPEN authorization must precede the native N093 intent, preventing credit for a use recorded between R03 review and R04 opening. Replay with pending intent is fail-closed.
- Signed scratch target is confined using `os.Root` and verified Windows physical paths. Latest patch added `releaseR04HoldSignedScratchUntilChild` with non-FILE_SHARE_DELETE handles for target **and all ancestors** during the native child, preventing a junction/rename swap after initial check. An important lifetime fix retains `*os.File` references until release, preventing finalizers from closing pinned handles early.
- Negative `TestR04SignedScratchHandlesBlockAncestorRenameWhileChildWouldRun` asserts parent rename fails while handles are held. Existing negative tests cover NTFS junction, use-before-R04, rollback, falsified unsigned results and no duplicate LEARN use.

## Evidence and quality, do not overclaim

- Last 13-file snapshot: full `go test ./...`, `go vet ./...`, `go build` PASS. Independent Sol medium on that older snapshot returned **CHANGES_REQUIRED, P0=0/P1=1** because physical scratch ancestors could be swapped before child execution. This was a real defect, not a cosmetic issue.
- Latest 15-file snapshot (including two new Windows hold files): focused R03/R04 tests, Go vet and Go build RC0, exact SHA-frozen source. Primary machine receipt: `R:\-4-\air-worker\work\R04_B_HANDOFF_20261020\QA_TERMINAL.json`. Candidate binary SHA is read from that receipt; it is NOT the installed binary.
- Read-only PLAN receipt: `R:\-4-\air-worker\work\R04_B_HANDOFF_20261020\PLAN_READBACK.json`, RC0 all checks, no manual changes to PLAN, plan/N-* or LEARN.
- **Remaining release gates:** no post-fix independent Sol-medium judgement on this exact 15-file source, no full-current-source Go test, and no fresh one-source real production R02→R03→R04 sequence. The current R04 code must be considered WIP, not SOURCE ACCEPTED, until those requirements pass.

## Next session — shortest measured path

1. Via AIR Commander `node: SRVLM01`, check current Git HEAD/branch/dirty and GitHub remote SHA; inspect this handoff, the JSON handoff and both SHA-pinned external receipts. Ensure no concurrent changes by line A.
2. Review only the *latest P1 fix* (Windows handles held through native child and the regression); use one independent `gpt-6-sol:medium` source judge, not repeated Astra high. Fail/repair only on real P0/P1.
3. Run one full `go test -count=1 ./...`, `go vet ./...`, `go build` only on frozen current SHA, collect machine receipt and hashes. No rerunning historical B2/B3 or reviewer jobs.
4. After judge+Go QA PASS, run a **new isolated disposable** native R02 → R03 → R04 on one exact source HEAD and candidate binary SHA, with one actual production Codex reviewer, one APPLIED and exactly one later `procedure_used`/bounded effect. Prior orchestration prototype: `R:\-4-\air-worker\work\R04_RELEASE_20261019\RUN_NATIVE_E2E_ALL_ONCE.ps1`; it was prepared for a PREVIOUS binary and contains stale paths/placeholders — **rebind and verify**, never run unchanged or repeat an already-started stage.
5. Store factual receipt, carry out any further scoped source-only commit + GitHub push with exact remote SHA. Coordinate actual release-plan integration with the designated **single native PLAN writer** of line A. Never edit PLAN.md, plan/N-*, or LEARN directly.
6. R05–R07 are separate final release/approval gates. For installation/publishing/tagging, reboot, UAC or stopping foreign services obtain a new explicit LPR 'да'; preserve rollback to installed 0.11.8.

## Safety, work isolation and rollback

Branch B source-only. No changes to line A, DEV136, the old dirty F:\-8-\air-modules worktree, production LEARN, installed plugin, services or actual release-slices. If final R04 gate fails, retain this scoped source commit as WIP or revert that exact commit; do not install or silently advance canonical release status. This handoff deliberately labels outstanding checks rather than falsely claiming PASS.
