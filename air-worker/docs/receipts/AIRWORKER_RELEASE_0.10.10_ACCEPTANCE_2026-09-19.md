# AirWorker 0.10.10 — release acceptance receipt

Date: 2026-09-19
Outcome: PASS

## Canonical release identity

- Repository: `ruhorh66-rgb/air-plugins`
- Release commit: `6a1cabd28601ba4e1dfa88a8857cd4d6d3a06cf2`
- Branches read back from GitHub:
  - `main`: `6a1cabd28601ba4e1dfa88a8857cd4d6d3a06cf2`
  - `release/air-worker-v0.10.10-hermes`: `6a1cabd28601ba4e1dfa88a8857cd4d6d3a06cf2`
- Annotated tag: `air-worker--v0.10.10`
- Tag object: `bab8befc187bdbcf668606155602ff381323788a`
- Peeled tag commit: `6a1cabd28601ba4e1dfa88a8857cd4d6d3a06cf2`
- GitHub Release ID: `391994745`
- Release URL: https://github.com/ruhorh66-rgb/air-plugins/releases/tag/air-worker--v0.10.10
- Release state: non-draft, non-prerelease

A pre-acceptance publication candidate exposed a real Hermes 0.21.3 installer incompatibility: its runtime loaded manifest v2, but `hermes plugins install` rejected versions above 1. The unaccepted Release ID `391988258` and tag object `26602ec1b1eb35ea1666c1fda009dfaaae0bcadb` were removed before final acceptance. The final source declares manifest v1, uses only v1-compatible fields, and passed the pinned remote installer path.

## Release assets

| Asset | Bytes | SHA-256 | GitHub asset ID |
|---|---:|---|---:|
| `air-worker-0.10.10-windows-x64.exe` | 7,638,016 | `7A62A2F47971DF7AD92C8B7A672C8E31FD54F4B0D490354B8DA76F816B2FC98A` | `574374730` |
| `air-worker-tray-0.10.10-windows-x64.exe` | 2,502,656 | `1662101AFD67E6102CACC89B9B74B9CF10B137A6105CE95C02AC01CDBAEF8CED` | `574374721` |
| `opencode-windows-x64-baseline-v1.18.31.zip` | 60,718,508 | `7C4FC9BE7124DF5E7C42184B99E8D8540FB0863BB0378B0C4219D9567B2D8434` | `574374719` |

GitHub API read-back reported each asset as `uploaded` and returned the same `sha256:` digest and byte size.

## Final source and static acceptance

Executed against the clean tagged worktree on SRVLM01:

- `go test ./... -count=1`: PASS (`airos/air-worker`, `airos/air-worker/tray`)
- `go vet ./...`: PASS
- Python plugin suite: 30/30 PASS
- `tools/check-binary.ps1`: PASS
- `tools/check-plugin.ps1`: PASS
- `tools/check-hermes-adapter.ps1 -Json`: PASS
- `.hermes/profiles/tests/Test-HermesProfileContracts.ps1`: PASS
- canonical `air-worker goals -json`: PASS, `problems: []`
- `git diff --check`: PASS
- clean worktree: PASS
- binary version: `air-worker 0.10.10`

The PowerShell judge parity oracle was brought back into agreement with the canonical binary for criterion counts, LPR gates, field sets, and human verdict text.

## Hermes acceptance

Runtime:

- Hermes Agent `0.21.3`, upstream `03fee43ca344ead7245a3b0ae20d38de0ae75642`
- Supported bounded path: query-file/stdin + oneshot + max-turns + run-budget
- Combined `chat --usage-file`: unavailable and deferred to PLAN step 94/K81

Pre-release isolated acceptance evidence:

- Continuity summary: `C:/Users/admin_loc/AppData/Local/hermes/canary/air-worker-0.10.10-continuity-20260919/CONTINUITY_CANARY_SUMMARY.json`
  - SHA-256: `0BFC5901F709DE08993B000FF646D7543A5AA9E0B41CA11B45F30FC15DE6E333`
- Bypass summary: `BYPASS_SUMMARY.json`
  - SHA-256: `689E48ECEDA7A77FB6F059585FFEC0A60736AF337AE6C549B0870E89D4AEE762`
- Enforce transcript: `enforce-continuity.jsonl`
  - SHA-256: `0552512618D928AA66C05A242F76317B989842296B58C1F8EDB3D1065077F6D2`

Assertions proved:

- baseline pending state: `0/2`, `needs_action`, `next_action=run_loop`, `stop_reason=no_live_worker`
- shadow mode remained read-only
- one enforce `status` call performed `0/2 → 1/2 → 2/2`
- the same call ran binary-owned loop and same-session judge verification
- final result: `verified=true`, `outcome=completed`, `next_action=none`, `stop_reason=plan_complete`
- no idle gap, continuity violation, premature completion, or `BLOCKED:` rewrite in the positive arm
- shadow allowed the disposable bypass mutation; enforce blocked it
- false RUNNING/PID reuse handling is covered by process-start-time identity tests

Final installed-release smoke after manifest compatibility correction:

- Profile: `airworker-hermes-v1-enforce`
- Session: `20260919_161757_40e2eb`
- One tool action: `status`
- Result: `verified=true`, `verification_action=verify`, `completed`, `2/2`, `plan_complete`
- Hermes process exit: 0
- Wall-clock duration: 20,353 ms (outer harness: 22,586 ms)
- Tokens: input 12,804; output 194; cache read 22,912; total 35,910
- Summary: `POST_RELEASE_SMOKE_SUMMARY.json`
  - SHA-256: `7F52FAC7D7848A19DDDBE436F91745C0426306BD3EDFB7500FCB512767190207`
- Transcript: `post-release-smoke.jsonl`
  - SHA-256: `DCBE75B733BA3BA819E0B0D1591873A9C4C12C2C276CF31D09013296C9DE442E`

The pinned remote installer command succeeded against the final commit:

`hermes plugins install ruhorh66-rgb/air-plugins/air-worker/.hermes/plugins/air-worker --ref 6a1cabd28601ba4e1dfa88a8857cd4d6d3a06cf2 --force --no-enable`

Default profile state after installation: AirWorker 0.10.10 installed from git, pinned to `6a1cabd2`, and not enabled. The default gateway remained running on PID 16072. Shadow and enforce profiles are separate distributions, both load AirWorker 0.10.10 successfully, and activation remains explicit by profile selection.

## Server installation

SRVLM01 live state:

- CLI version: `air-worker 0.10.10`
- Live CLI SHA-256: `7A62A2F47971DF7AD92C8B7A672C8E31FD54F4B0D490354B8DA76F816B2FC98A`
- Live tray SHA-256: `1662101AFD67E6102CACC89B9B74B9CF10B137A6105CE95C02AC01CDBAEF8CED`
- Tray restored in the existing interactive console session after installation; no autostart was added
- Claude user and project registrations: AirWorker 0.10.10, enabled
- Claude cache: `E:/-4-/claude-home/plugins/cache/air-plugins/air-worker/0.10.10`
- Codex registration: AirWorker 0.10.10, enabled
- Hermes default profile was not switched
- Hermes shadow and enforce profiles: AirWorker 0.10.10, Plugin Doctor PASS, 1 tool and 9 hooks

## Rollback rehearsal

An isolated, non-live Claude/Codex configuration installed 0.10.10 and then rolled back through a marketplace snapshot exported from annotated tag `air-worker--v0.10.9`:

- 0.10.9 peeled commit: `3a1df0d4a3ae0dc7e8b28c611db3322df2dc7dfa`
- Claude: 0.10.10 → 0.10.9 PASS
- Codex: 0.10.10 → 0.10.9 PASS
- Summary: `ROLLBACK_REHEARSAL_SUMMARY.json`
- SHA-256: `ED40F7740E6E90CA63B26506D7F879694B171F210D428833074C4E5EE0E5A8EB`

Live 0.10.9 caches were preserved. Rollback of the CLI remains the exact cached 0.10.9 `install -no-start` path; Hermes rollback is switching to `default` and leaving the pinned AirWorker plugin disabled.

## Isolation and protected product

- Credentials, memories, sessions, and cron were not copied into the release repository.
- Existing profile auth files and `.env` were preserved by profile installation; only AirWorker binary/mode/allow-list entries were updated.
- Default Hermes gateway remained running and the default profile remained selected.
- Scheduled Task `AIR-OS-StorageOrderSupervisor-20260918`: `Disabled`
- Processes linked to `F:/-0-/AIR_OS/04_AI_OPERATIONS/WOODY_RUNS/srvlm01-airenv002-storage-order-0109`: 0
- The protected storage-order product was not modified, rolled back, deleted, or executed.
