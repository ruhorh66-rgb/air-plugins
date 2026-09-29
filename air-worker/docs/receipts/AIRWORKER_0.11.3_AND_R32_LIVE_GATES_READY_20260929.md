# AirWorker 0.11.3 + AIR Commander r3.2 live cutover gate ready — 2026-09-29

Node: SRVLM01
Status: READY / LIVE INSTALLATION NOT EXECUTED

## AirWorker 0.11.3 release

Published:
- GitHub repository: ruhorh66-rgb/air-plugins
- tag: air-worker--v0.11.3
- package commit: 05447b5598573aea88c1b94fb41281b4e2f97a51
- embedded clean source: 9e93be6621aae44c0c26c66398213eb39553b68d
- GitHub Release: AirWorker 0.11.3

Assets:
- air-worker-0.11.3-source.zip
  - SHA-256 CC5F0896914976A6E2095F194F677789D7E726D80B0359C2D8980F8B070470E2
- air-worker-0.11.3-windows-x64.exe
  - SHA-256 35D285BDF0464DEBE5DDB303FC9C88658482E1695C5B654D9B722A12E3324C53
- air-worker-tray-0.11.3-windows-x64.exe
  - SHA-256 AE9697E3B9BE10AE18C96C096A217C530A2D70AAC2CD81122A564782C23C6F33

GitHub asset sizes and digests match local release artifacts.

Package gate:
- Go test PASS
- go vet PASS
- plugin PASS
- Hermes 8/0
- ladder 10/0
- engine 14/0
- planner PASS
- binary PASS
- boundary PASS
- shell PASS
- reproducibility 43/0
- diff-check PASS

## Current SRVLM01 live AirWorker

Still unchanged:
- live version 0.11.2
- revision b3cce88bd9cdb5e19655512755fad61bee1ec0a8
- CLI SHA-256 CC12F82CF79A6EB183D065D0EE604DD9585DC5B9070B172FEDE44A13534ACEE5
- selfcheck warnings=0 / violations=0 / not_proven=0
- canonical Claude/Codex GitHub plugin sources present

No 0.11.3 live installation has been performed.

## AIR-ENV-002 preinstall facts

AIR Commander Test registry reports the node online.

The first process probe returned EXECUTION_UNKNOWN and was not blindly retried.

Read-only filesystem/registry inspection:
- live CLI file size: 8,973,312 bytes
- live tray file size: 2,527,744 bytes
- those sizes equal the published 0.11.2 release assets
- user Codex cache contains AirWorker 0.11.2
- R:/-4-/claude-home installed_plugins.json points AirWorker 0.11.1
- C:/Users/admin_loc/.claude installed_plugins.json points AirWorker 0.10.9
- both inspected Claude marketplace registries still name GitHub ruhorh66-rgb/air-plugins
- R:/-4- Claude/Codex caches are therefore not a trustworthy single source of active-version truth

N-026 records this profile drift. During the approved 0.11.3 install, profiles must be normalized only through official Claude/Codex plugin commands; no registry JSON edits by hand.

## AirWorker rollback proof

Rollback target:
- air-worker--v0.11.2
- package commit d33424d089fd8576980be5eb09af9eb4d36a1751

Release assets are still present on GitHub:
- CLI SHA-256 CC12F82CF79A6EB183D065D0EE604DD9585DC5B9070B172FEDE44A13534ACEE5
- tray SHA-256 CF68DB8C49922D0F92189861D69C4A1F470768B69E39E2007EE601ECEA3E860A
- source ZIP SHA-256 FD8E553EA56A6BE455E5230537563DD6C945453D2D39946B2DD891CA150F2FA5

Disposable rollback probes proved both host CLIs can install the exact old version from GitHub tag without hand-editing registries.

Claude disposable proof:
- marketplace source: ruhorh66-rgb/air-plugins@air-worker--v0.11.2
- plugin installed: air-worker@air-plugins 0.11.2
- gitCommitSha: d33424d089fd8576980be5eb09af9eb4d36a1751
- disposable CLAUDE_CONFIG_DIR removed after test

Codex disposable proof:
- marketplace source: ruhorh66-rgb/air-plugins
- ref: air-worker--v0.11.2
- plugin installed: air-worker@air-plugins 0.11.2
- disposable CODEX_HOME removed after test

Therefore rollback can use official GitHub marketplace/tag mechanisms rather than manual registry edits.

## Signed updater feed

The AirWorker Ed25519 private signing key exists at the machine-local secret path:
R:/-4-/air-worker/secrets/update-ed25519-private.b64

Its value was not read or printed.

The attempt to pass the signing-key argument to publish-github-update.ps1 was blocked by the execution safety layer. The protection was not bypassed.

This does not block the approved GitHub marketplace installation path.

## AIR Commander r3.2 ChatGPT hook release

Published:
- repository: ruhorh66-rgb/air-commander
- tag/release: r3.2-2026-09-29
- source implementation: 8b6a3cc069155e8669b1d044171eb9fca9067194
- bridge version: 0.1.2
- source acceptance closure: 9750e29c40367d48ca6600d0cf7c0a5756c0a462

Fresh GitHub tag checkout:
F:/-8-/_releases/air-commander-r3.2-2026-09-29

Acceptance:
- checkout HEAD = 8b6a3cc069155e8669b1d044171eb9fca9067194
- clean tree
- bridge version 0.1.2
- bridge regression: 99 total / 98 PASS / 0 FAIL / 1 Windows symlink fixture skip

It preserves the live r3.1 ProcessOutputArchive fix and adds:
- air_worker_attach
- air_worker_status
- air_worker_pending
- air_worker_finalize
- private/app-only air_worker_approve
- ChatGPT SessionStart
- automatic PreToolUse/PostToolUse
- Stop
- trusted UserPromptSubmit approval
- minimal transcript without tool arguments

The unrelated r4 worktree was inspected and rejected as a cutover target because it is not a safe superset of live r3.1: it does not preserve the r3.1 process-output archive lineage and remains bridge version 0.1.0.

## Current AIR Commander live rollback point

Live main bridge:
- AIR Commander r3.1
- bridge 0.1.1
- working directory F:/-8-/_releases/air-commander-r3.1-2026-09-29
- managed by ASW devspace-bridge
- rollback from r3.2 is one working_directory restoration plus ASW air-commander restart

No r3.2 restart/install has been performed.

## Explicit LPR gates still required

### Gate A — AirWorker 0.11.3 live install

What:
- update canonical GitHub air-plugins marketplace(s) through official Claude/Codex CLI;
- install/enable air-worker@air-plugins 0.11.3 in intended active profiles;
- install live user-scope AirWorker from the canonical GitHub plugin cache;
- normalize AIR-ENV-002 profile drift through the same official CLI path.

Why:
- close N-019, N-026 and then parent N-014;
- make executor list/dispatch, GPT-6 ladder, Headroom judges and Ponytail transport live.

Rollback:
- before mutation capture current live/profile state;
- official Claude/Codex marketplace pinned to GitHub tag air-worker--v0.11.2;
- reinstall 0.11.2 plugin from that pinned GitHub source;
- restore live 0.11.2 binary/tray from canonical 0.11.2 cache/release path;
- verify selfcheck zero warnings/violations/not_proven.

Acceptance:
- AirWorker version 0.11.3;
- exact release revision/SHA;
- Claude/Codex plugin 0.11.3, GitHub source;
- Headroom preflight PASS;
- Ponytail preflight PASS;
- executor list PASS;
- GPT principal/session/dispatch smoke PASS;
- SRVLM01 and AIR-ENV-002 selfcheck PASS.

UAC: NO expected
Reboot: NO
Foreign services: NO

### Gate B — AIR Commander r3.2 live hook cutover

What:
- atomically change only devspace-bridge.working_directory from r3.1 release checkout to r3.2 release checkout;
- restart only through ASW air-commander restart;
- Refresh AIR Commander in ChatGPT and use a new chat/session.

Why:
- close N-004 and enable automatic ChatGPT AirWorker lifecycle parity.

Rollback:
- restore r3.1 working_directory;
- ASW air-commander restart;
- verify r3.1 bridge 0.1.1 PID/lock/8730/health.

Acceptance:
- new bridge PID;
- ASW desired=running;
- PID == lock PID == port 8730 owner;
- health version 0.1.2 RUNNING/OK;
- air_worker_attach/status/pending/finalize visible after Refresh;
- private approve remains app-only;
- live attach -> SessionStart -> harmless tool -> PreToolUse/PostToolUse -> finalize/Stop PASS;
- private approval grant path PASS.

UAC: NO
Reboot: NO
Foreign services: NO
