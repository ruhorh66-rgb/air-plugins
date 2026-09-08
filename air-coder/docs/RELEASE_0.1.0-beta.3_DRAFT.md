# AirCoder 0.1.0-beta.3 — release dossier DRAFT

Status: PREPARED, NOT RELEASED
Prepared: 2026-09-08
Release type: prerelease
Intended tag: `air-coder--v0.1.0-beta.3`
Rollback tag: `air-coder--v0.1.0-beta.2`
Rollback commit: `f7e9020146cca41015c1ed4832bb317f4126cd4b`

## Delivered candidate

- Thin ready-executor runner using native `codex exec` / `codex exec resume`; no custom agent loop.
- Fail-closed context, repository, allowed-path and protected-path gates.
- Staged + unstaged `git diff --check` coverage and bounded repair.
- Persisted timing and resume state with no automatic replay of uncertain in-flight calls.
- Resume task-id validation before state-path construction, with the same safe-segment rule enforced by JSON Schema and runtime validator.
- Ordinary implementation routes to ready Codex by default; size alone no longer forces Ruflo.
- Ruflo remains explicit for substantial implementation signs through `air-ruflo-bridge`.
- Bridge hardening for invalid queue timestamps and non-object approval request files.

## Mechanical acceptance

- AirCoder unittest: 53/53 PASS.
- Draft 2020-12 coding-task schema validation and task-id schema/runtime parity: PASS.
- AC04 queue invalid-time regression: PASS.
- AC04 listener non-object regression: PASS.
- Listener signature/security checks: 8/8 PASS.
- Listener liveness checks: 10/10 PASS.
- Queue regression: 7/7 PASS.
- `git diff --check`: PASS.
- Claude plugin validation: PASS.
- Shared marketplace validation: PASS.
- Version parity across product/Codex/Claude manifests: `0.1.0-beta.3` PASS.
- All 14 AirCoder JSON files parse: PASS.
- Both Draft 2020-12 contract schemas validate: PASS.
- Python `compileall` for AirCoder: PASS.

## Installability evidence

An isolated test consumer was created under `E:/-4-/air-coder/install-smoke-schema`; production user installs remained beta.2.

- Codex isolated marketplace/add installed `air-coder@air-plugins` version `0.1.0-beta.3`: PASS.
- Claude isolated local-scope install installed version `0.1.0-beta.3`: PASS.
- Source / isolated Codex / isolated Claude SHA-256 parity: PASS for selector, runner, skill instruction, product manifest and coding-task schema.
- Installed selector smoke on both caches: ordinary implementation -> `native_cli`, target `native CLI (codex)`: PASS.

## AC-04 evidence accounting

Formal native-executor pilot remains 3/5 accepted: 001, 002, 003B. 004B remains an honest timeout receipt; 005B remains an honest usage-limit receipt. No false-ready result and no automatic paid retry occurred.

LPR-authorized manual execution fixed and independently verified the two nonaccepted product defects. This functional evidence is additive and does not rewrite the original executor receipts.

## Remaining release gates

1. Real native-executor/live interruption-resume proof remains outstanding.
2. Formal AC-04 acceptance is still below the original 4/5 target unless LPR explicitly changes the acceptance policy.
3. Default-branch merge, tag, production marketplace update/install and final smoke require the release decision.

## Intended release sequence after gate opens

1. Re-run final mechanical/manifest validation on the exact release commit.
2. Merge approved beta.3 candidate to default branch through the normal release path.
3. Create `air-coder--v0.1.0-beta.3` only from the approved merged source.
4. Update marketplace snapshots, then update/install AirCoder on Codex and Claude Code.
5. Verify reported version, selector/runner SHA parity and ordinary-entry smoke on both hosts.
6. On failure, restore `air-coder--v0.1.0-beta.2`; preserve beta.3 runtime receipts for diagnosis.
