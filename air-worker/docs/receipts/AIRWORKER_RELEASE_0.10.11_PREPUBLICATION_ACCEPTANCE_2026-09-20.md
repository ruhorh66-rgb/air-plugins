# AirWorker 0.10.11 — pre-publication acceptance receipt

Date: 2026-09-20 01:08:52 +08:00
Outcome: PASS
Publication / installation / activation: BLOCKED pending separate LPR

## Candidate identity

- Repository: `ruhorh66-rgb/air-plugins`
- Branch: `gpt/air-worker-0.10.11-p1-20260919`
- Accepted source candidate: `d5824e706e6b022af29a45041b150f93138e941e`
- Release packaging/tag target: `adfb7cac8bc5bbe6e5318490603499a9fa6f4fca`.
- Required base/ancestor: `33edba96ef736baf609bc98fd076de0c69c46300`
- Scope authority: ARCH v0.3 at `9726f6527ad4c7e211f8d2633f1b70565284f3b3`
- Accepted scope: only Ц7-1…Ц7-7 / three P1 corrections
- Worktree at acceptance: clean

## Artifacts

| Asset | Bytes | SHA-256 |
|---|---:|---|
| `air-worker-0.10.11-windows-x64.exe` | 7,647,232 | `0F90B44D120E7587661A42438345DB9C0AD054FD3FD094909781649E4F719A21` |
| `air-worker-tray-0.10.11-windows-x64.exe` | 2,502,656 | `F86D1517D6A37BC690732AC22E13E621039A31076452E4499CD07C1E3692C895` |

Staged package root: `F:\-7-\_packages\air-worker-0.10.11-repo-candidate\air-worker`.
The staged package was created from the exact committed candidate with `git archive`; all 235 tracked AirWorker files were present, with no missing or extra source files. Both staged binaries matched the standalone candidate artifacts byte-for-byte.

## Deterministic acceptance

- Full staged-package `go test ./... -count=1`: PASS (`airos/air-worker`, `airos/air-worker/tray`).
- `go vet ./...`: PASS.
- Targeted Ц7 regressions: PASS.
- Python Hermes plugin suite using `unittest discover`: 30/30 PASS.
- `tools/check-binary.ps1`: PASS; binary reports `air-worker 0.10.11` and judge/engine parity checks pass.
- `tools/check-plugin.ps1`: PASS; Claude/Codex/Hermes manifest and binary version parity is `0.10.11`.
- `tools/check-hermes-adapter.ps1 -StaticOnly`: PASS, including Hermes Plugin Doctor.
- `.hermes/profiles/tests/Test-HermesProfileContracts.ps1`: PASS.
- `git diff --check 33edba96ef736baf609bc98fd076de0c69c46300..d5824e706e6b022af29a45041b150f93138e941e`: PASS.

## Semantic acceptance

Hermes-native independent reviewer:

- Delegation: `deleg_b1dce94e`
- Subagent: `sa-0-45849c27`
- Model/provider: `gpt-5.6-sol` / `openai-codex`
- Verdict: PASS
- Scope compliant: true
- Artifact hashes verified: true
- Blocking findings: none

Proven behaviors:

- An open or malformed gate-like plan row stops loop/orchestrate fail-closed, names the gate with exact `ЖДЁТ ЛПР`, returns code 3, and does not execute downstream judge/script/model work.
- Closed gates allow following work.
- Every production Windows PowerShell 5.1 launch uses the centralized sanitized child environment; PowerShell 7 (`pwsh`) remains unchanged; Get-FileHash integration passes.
- A verified live semantic reviewer remains visible after its reviewed step closes; adapter reports `running`/`wait`, preserves role/provider/model/sandbox metadata, and enforce does not relaunch.
- PID/start-time correlation and PID-reuse defenses remain mandatory and green.
- Quarantine stdin transport, campaign redesign, reviewer-selector changes, storage-order scope, and Ц8–Ц11 did not enter runtime implementation.

## Publication read-back

Publication was explicitly authorized by LPR and completed at 2026-09-20 01:25 +08:00.

- Main: `adfb7cac8bc5bbe6e5318490603499a9fa6f4fca` at publication.
- Release branch: `release/air-worker-v0.10.11-hermes` → `adfb7cac8bc5bbe6e5318490603499a9fa6f4fca`.
- Annotated tag: `air-worker--v0.10.11`.
- Tag object: `6cb76646f724629f4411827807718f9bb4abac5c`.
- Peeled tag commit: `adfb7cac8bc5bbe6e5318490603499a9fa6f4fca`.
- GitHub Release ID: `392157169`.
- URL: https://github.com/ruhorh66-rgb/air-plugins/releases/tag/air-worker--v0.10.11
- Release state: non-draft, non-prerelease.
- Asset `air-worker-0.10.11-windows-x64.exe`: ID `575174440`, `uploaded`, 7,647,232 bytes, digest `sha256:0f90b44d120e7587661a42438345db9c0ad054fd3fd094909781649e4f719a21`.
- Asset `air-worker-tray-0.10.11-windows-x64.exe`: ID `575174442`, `uploaded`, 2,502,656 bytes, digest `sha256:f86d1517d6a37bc690732ac22e13e621039a31076452e4499cd07c1e3692c895`.
- Asset `opencode-windows-x64-baseline-v1.18.31.zip`: ID `575174441`, `uploaded`, 60,718,508 bytes, digest `sha256:7c4fc9be7124df5e7c42184b99e8d8540fb0863bb0378b0c4219d9567b2d8434`.

Installation, activation, service changes, and live process replacement were not performed and remain outside this publication LPR.
