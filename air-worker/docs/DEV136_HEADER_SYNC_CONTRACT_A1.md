# DEV136 · AW-DEV136-A1 · Native PLAN header sync

Status: SOURCE-ONLY / PRE-RELEASE / N124 GLOBAL NODE AUTHORITY PENDING.
Source: AirWorker_Wiki issue #1, AirLegal N-111, LPR decision 17.10.2026.

## Scope

Maintain a bounded, machine-owned header only. Existing Markdown and node files remain owned by their writers. No automatic migration or reformatting of legacy PLAN.md. Header marker contract:

<!-- air-worker-managed-header:start -->
Git branch, source HEAD, installed version (attested), active release, next OPEN node
<!-- air-worker-managed-header:end -->

Native command proposed: air-worker plan header sync -product <root> -json -dry-run.
Mutation requires -apply, expected branch, HEAD, and PLAN SHA, plus an evidence file proving installed binary/version. Missing authority or evidence means BLOCKED (nonzero), not a guessed PASS.

## Acceptance contract

- Read-only Git HEAD/branch/remote compare; version evidence tied to installed binary SHA; numeric SemVer and release-slice sourced from a machine file, not prose.
- Global N-ID uniqueness through N124; until N124 PASS, reject ambiguous/private staged nodes.
- Check UTF-8/C0, markers exactly once, preserve rest of PLAN byte-for-byte, refuse unsafe symlink/reparse paths.
- Named mutex shared with native plan node writes; before commit reread PLAN/Git and verify exact generation, then atomic write+readback.
- Repeat no-op neither mutates PLAN nor writes a spurious LEARN entry.
- JSON status and before/after SHA, changed fields, rc, BLOCKED reasons.
- Negative tests: stale HEAD, missing remote, concurrent PLAN edit, missing evidence, malformed header, duplicate nodes, private WIP, corrupted Unicode, repeated apply.
- Stable 0.11.8 untouched, rollback to its published SHA required before future install.
- N-134 remains STAGED_PRIVATE and contains C0 U+0007 in another worktree; no edit of that node or LEARN here.

This first slice is a safe source-only implementation/test baseline, not an official AirLegal promotion or published release.


## Independent review gate — source-only A1

First source commit `488d3888c875047ffe63de36348fb78be09724ee` had Codex `gpt-6-sol:medium` review `passed=false`: P1 concurrent external PLAN edit during replace; P1 junction/reparse ancestor; P2 marker location inside prose or fenced block. The reported full Go tests/vet/build were rc0 but do not override this verdict.

Second microstep adds RED/GREEN regression for markers embedded in text/code and requires markers on standalone lines within the H1 preamble before any `##` section or fenced code. While native N124 generation and pinned-root write exclusion remain unaccepted, **any changed-bytes publication via `writeManagedPlanHeaderCAS` is disabled with nonzero error**. Read-only transform/dry-run logic and exact-no-op remain available; no CLI route is wired or released. This is a deliberate safety hold, not a completed header sync.

The tracked Git HEAD can become stale immediately after committing a PLAN header containing its precommit SHA. Future native command must define source SHA as an *observed generation at sync* with a separate postcommit receipt; it must never claim a tracked text SHA is self-updating. Before enabling `-apply`: OS-level root/ancestor reparse guards, race-safe publishing, pinned installed-version attestation, exact remote authority, N124 and independent Sol PASS required.


## Source checkpoint A2 — native CLI preview, safety before integration

Native dispatcher `air-worker plan header sync -product <root> -dry-run -json [-remote origin]` now exists **in the DEV136 development branch only**. It inspects the canonical Git branch and source HEAD, reads an explicitly selected HTTPS GitHub remote branch HEAD, examines native N-ID consistency and active `plan/release-slices.json`. It reports machine-readable `BLOCKED` with nonzero exit code rather than inventing installed version evidence. Git reads are bounded in time and cannot use interactive credential prompts. Source trees and all PLAN/N/LEARN files are read-only under this command.

**Safety:** the `-apply` flag is rejected. The helper `writeManagedPlanHeaderCAS` preserves byte-identical NO_CHANGE only; changed bytes always fail closed. This remains necessary because independent Sol reviewers found P1 unsafe cross-process publication and Windows ancestor junction risks. N124 global generation and a trusted installed-product version/binary attestation have not yet been accepted. A preview PASS never means an applied PLAN update, and `BLOCKED` is the only current native outcome.

**TDD:** prepatch RED for `cmdPlanHeaderSync` undefined; after implementation targeted `TestPlanHeader*` PASS. Additional adversarial marker cases with indented code and nested headings RED before patch and GREEN after. No product PLAN, N-* or LEARN file was altered manually. Release and installed 0.11.8 unchanged.

**Still required to close N-111:** integration with accepted N124 atomic Git authority (two-client CAS and durable outbox), reparse-safe pinned PLAN root/OS handle; source provenance and signed installed-version evidence; exact-SHA semantic reviewer PASS; full QA, independent release version, rollback 0.11.8, future explicit LPR approval for install. No delivery-ready `plan header sync -apply` exists as of A2.


## A2 executable preview smoke (SRVLM01)

Built with Go 1.26.8, `GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local`, `go build -trimpath -buildvcs=false`; candidate binary SHA256 `5f293b41570b2e0a313f807e725042d1993bb71c83b991c543a7d0e7f19e66f2`. A2 CLI build and dedicated `TestPlanHeader*` are PASS. Full module regression and new independent judge on A2 SHA remain PENDING while separate Hermes N124 Go tests are running.

Actual `plan header sync -product <this isolated AirWorker root> -dry-run -json -remote github` returned rc 2 `BLOCKED` (as designed), verified `git_head=remote_head=57a1bb624a7952c58273ed888fc6bc6edaf2fa4b`, branch `dev/airworker-dev136-header-sync-20261017`, next OPEN `N-028_0-11-4-operational-self-learning-procedures-for-s`, plan SHA256 `a488abc965dca2816df95c9043f3a62c393ca1a1769685d6f9b5bf51968e0e95`. Blockers: `active_release_not_proven` (0.11.8 has no release-slices), `installed_version_binary_attestation_not_implemented`, `N124_global_git_generation_not_accepted`. PLAN before/after SHA identical. This is deliberately **not** an installed AirLegal production test and grants no mutation.
