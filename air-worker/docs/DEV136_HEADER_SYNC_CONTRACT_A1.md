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
