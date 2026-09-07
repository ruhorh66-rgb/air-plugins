# AirCoder rollback — 0.1.0-beta.2

There is no previous AirCoder Stable.

Rollback target: pre-AirCoder state of `air-plugins` at `7dca858845a48a6347c05a91b22bdf8daaaf7775`.

## Procedure

1. Disable/remove the AirCoder marketplace entry and plugin directory from the candidate branch/release.
2. Verify `air-worker`, `air-ruflo-bridge`, `air-worker-codex`, Codex CLI and Claude Code files are unchanged by the AirCoder commit.
3. Continue using the existing executor routes directly.
4. Verify `git diff 7dca858845a48a6347c05a91b22bdf8daaaf7775..HEAD -- air-worker air-ruflo-bridge air-worker-codex` is empty for the AirCoder change set.

No runtime database or state migration exists, so rollback does not touch `E:\-4-` state.
