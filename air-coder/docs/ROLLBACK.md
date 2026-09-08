# AirCoder rollback — 0.1.0-beta.3 candidate

Previous released AirCoder: `air-coder--v0.1.0-beta.2`.

## Source rollback

1. Restore/install beta.2 from its release tag; do not recover from a working-directory path.
2. Verify AirCoder manifests/product contract report `0.1.0-beta.2`.
3. Verify the installed selector executes on both intended hosts before declaring rollback complete.
4. Keep `air-ruflo-bridge`, AirWorker, Codex CLI, Claude Code and LM Router unchanged by the AirCoder rollback.

## Runtime/state rollback

Beta.3 adds only task contracts/receipts under `AIR_CODER_RUN_ROOT` (default `E:/-4-/air-coder/runs`). These are evidence, not a migration database. Rollback does not delete them.

Do not automatically resume an `executor_running` or `repair_running` receipt after rollback; its execution state is uncertain and requires explicit inspection.

## Release gate

Before beta.3 installation, record the exact beta.2 tag as rollback target and prove the install/update path on a copy/test consumer. A green source test suite alone is not rollback evidence.