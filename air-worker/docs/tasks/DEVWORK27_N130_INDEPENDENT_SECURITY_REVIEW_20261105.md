# N-130 / Dev Work 27 — independent source-security review of AirWorker rc.4

Owner: Dev Work 27, SRVLM01. Reviewer only (read-only); not an implementation assignment.
Scope: current clean Git HEAD on branch dev/airworker-native-orch-pilot-20261104, baseline bf2ee4afd48aad9038d0d552c55a21af5a01d7de, plus THIS review task commit. Check git rev-parse HEAD and git status before claims. Never confuse runner exit RC0 with a semantic PASS.

Source and exact paths to inspect:
1. cmd/bypassguard.go, cmd/bypassguard_test.go, cmd/hook.go: compound shell strings with an authorized Claude marketplace prefix followed by semicolon/ampersand/pipeline, and denied direct codex/claude. Ensure no unguarded alternate entry, prove nonzero return.
2. cmd/semantic.go, cmd/semantic_process_exit_test.go: Claude reviewer process exits nonzero but emits a JSON PASS; empty, malformed and is_error payloads cannot close.
3. cmd/update_cache_transaction_windows.go, cmd/update_cache_transaction_windows_test.go, cmd/update_windows.go, cmd/update.go, cmd/distribution_identity.go: official GitHub signed updater; caller lock scope, partial failure while syncing both Claude/Codex caches, rollback of metadata and previous version binaries, original file absence, preserving unrelated/foreign plugin state. Inspect symlink, NTFS junction/reparse point in cache TARGET, any parent directory, nested entries, metadata, source and destination, races between validation and removal, and Windows ACL limitation.
4. cmd/update_identity.go, tools/publish-github-update.ps1: authentic vcs.revision, clean Git release build, Ed25519 existing signing key; no bypass, no new trust root.
5. .claude-plugin/plugin.json, .codex-plugin/plugin.json, .hermes/plugins/air-worker/plugin.yaml, hooks/hooks.json: three host package contracts and delivery assumptions, with installed-host proof marked separately.
6. run-config.json, cmd/orchestration.go, cmd/runner_codex.go, cmd/release_slice_runner.go: require_native_subagents/require_anthropic_reviewer and historical R0.11.9-05, no bypass/numeric fallback.

Reviewer MUST NOT alter files, execute installer, edit native PLAN/N/LEARN, fetch secrets, use PAYG API, or bypass an access/security refusal. No direct Codex App Server. State what files were actually readable; when missing say TASK_READ_FAIL and NOT_PROVEN.

Output verdict EXACTLY one of: PASS_SOURCE_ONLY / FAIL_P0 / FAIL_P1 / NOT_PROVEN. With precise path:line references, up to 8 concrete findings and minimal correction. Mark windows symlink SKIP as SKIP, not PASS. Explicitly distinguish source tests from proven runtime install. Conclude TASK_READ_OK only after reading this task and actual source.
