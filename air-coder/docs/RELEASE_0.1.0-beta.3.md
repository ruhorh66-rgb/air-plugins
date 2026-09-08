# AirCoder 0.1.0-beta.3

Release status: PACKAGE ACCEPTED FOR TAG
Release date: 2026-09-08
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

- AirCoder unittest: 54/54 PASS.
- Draft 2020-12 coding-task schema validation and task-id schema/runtime parity: PASS.
- Limit contract hardening: strict integer values, positive executor/check timeouts, bounded repairs and unknown-field rejection: PASS.
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

An isolated test consumer was created under `E:/-4-/air-coder/install-smoke-limits`; production user installs remained beta.2.

- Codex isolated marketplace/add installed `air-coder@air-plugins` version `0.1.0-beta.3`: PASS.
- Claude isolated local-scope install installed version `0.1.0-beta.3`: PASS.
- Source / isolated Codex / isolated Claude SHA-256 parity: PASS for selector, runner, skill instruction, product manifest and coding-task schema.
- Installed selector smoke on both caches: ordinary implementation -> `native_cli`, target `native CLI (codex)`: PASS.

## AC-04 evidence accounting

Formal native-executor pilot is 4/5 accepted: 001, 002, 003B, 005B. 004B remains an honest timeout receipt. The first 005B usage-limit receipt is preserved in the archive. False-ready=0; automatic paid retries after infrastructure failure=0.

LPR-authorized manual execution fixed and independently verified the two nonaccepted product defects. This functional evidence is additive and does not rewrite the original executor receipts.

## Release authorization

LPR authorized the beta.3 release sequence on 2026-09-08. Product changes merged to `main` at `1dd9fb47d2b11066caa12f70ccc8618d393e7cf1`. This release commit records package acceptance for tag creation; production install/smoke evidence is recorded after the tag and is not pre-claimed here.

## Intended release sequence after gate opens

1. Re-run final mechanical/manifest validation on the exact release commit.
2. Merge approved beta.3 candidate to default branch through the normal release path.
3. Create `air-coder--v0.1.0-beta.3` only from the approved merged source.
4. Update marketplace snapshots, then update/install AirCoder on Codex and Claude Code.
5. Verify reported version, selector/runner SHA parity and ordinary-entry smoke on both hosts.
6. On failure, restore `air-coder--v0.1.0-beta.2`; preserve beta.3 runtime receipts for diagnosis.

## Final Astra gate status ? 2026-09-08

- Live interruption/resume: PASS; one executor turn, same thread, no replay.
- R4 ordinary chat entry: PASS; session inferred route facts and completed Codex runner 3/3.
- AC-04: 4/5 accepted, false-ready=0.
- Economics: 8 preserved native calls across current/replaced AC-04 attempts, 0 repairs, 35.190 receipt-minutes; direct cost unknown.
- Product merge complete at `1dd9fb47d2b11066caa12f70ccc8618d393e7cf1`; release package accepted for tag. Production update/smoke follows after tag.

## Production rollout — 2026-09-09

- Release tag `air-coder--v0.1.0-beta.3` points to `a760a0c6b024426d6ef43a0ab9baa5a0f26d507b`.
- Codex marketplace refresh and plugin cache update to `0.1.0-beta.3`: PASS.
- Claude marketplace/plugin update from beta.2 to beta.3: PASS; already-running Claude sessions require restart, new sessions load beta.3.
- Exact tag source / Codex production cache / Claude production cache SHA-256 parity: PASS for selector, runner, skill, product manifest and coding-task schema.
- Installed ordinary selector smoke on both production caches: `native_preference=auto` -> `native_cli`, target `native CLI (codex)`: PASS.
- Local beta.2 rollback snapshot: `E:/-4-/air-coder/rollback-beta2-pre-beta3`; canonical rollback tag remains `air-coder--v0.1.0-beta.2`.
