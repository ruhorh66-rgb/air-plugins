# AirWorker 0.11.3 package + GitHub release acceptance — 2026-09-29

Node: SRVLM01

Release identity:
- source commit embedded in binaries: `9e93be6621aae44c0c26c66398213eb39553b68d`
- package commit/tag target: `05447b5598573aea88c1b94fb41281b4e2f97a51`
- tag: `air-worker--v0.11.3`
- GitHub Release: `AirWorker 0.11.3`
- rollback tag: `air-worker--v0.11.2`

Package binary facts:
- CLI version: `air-worker 0.11.3`
- CLI embedded vcs.revision: `9e93be6621aae44c0c26c66398213eb39553b68d`
- CLI vcs.modified: `false`
- tray embedded vcs.revision: same source commit
- tray vcs.modified: `false`

Release assets and matching GitHub digests:
- `air-worker-0.11.3-source.zip`
  - bytes: 66440230
  - SHA-256: `CC5F0896914976A6E2095F194F677789D7E726D80B0359C2D8980F8B070470E2`
- `air-worker-0.11.3-windows-x64.exe`
  - bytes: 9038336
  - SHA-256: `35D285BDF0464DEBE5DDB303FC9C88658482E1695C5B654D9B722A12E3324C53`
- `air-worker-tray-0.11.3-windows-x64.exe`
  - bytes: 2527744
  - SHA-256: `AE9697E3B9BE10AE18C96C096A217C530A2D70AAC2CD81122A564782C23C6F33`

GitHub Release was published and its asset sizes/digests match these local artifacts.

## Full package gate

- `go test . -count=1 -timeout 180s`: PASS
- `go vet ./...`: PASS
- `check-plugin.ps1`: PASS
- Hermes adapter: 8 PASS / 0 FAIL
- `check-ladder-reachable.ps1`: 10 PASS / 0 FAIL
- `check-engine.ps1`: 14 PASS / 0 FAIL
- `check-planner.ps1`: PASS
- `check-binary.ps1`: PASS
- `check-boundary.ps1`: PASS
- `check-shell-compat.ps1`: PASS
- `check-reproducible.ps1`: 43 PASS / 0 FAIL
- `git diff --check`: PASS

## Release-check tails closed

### N-022 — stale llm-queue release check

The old release check still required the removed llm-queue dispatcher. It was aligned to the approved 0.11.3 architecture and now proves:
- packaged AirWorker resolves Ponytail transport;
- packaged AirWorker resolves Headroom transport;
- packaged AirWorker exposes the approved OpenAI executor ladder.

Acceptance: `check-ladder-reachable.ps1` = 10 PASS / 0 FAIL.

### N-023 — engine check vs judgeDetailed

The old release check counted only textual `c.judge()` calls while the current loop uses initial `c.judge()` plus per-iteration `c.judgeDetailed()`.

Acceptance: `check-engine.ps1` proves initial factual judge + repeated detailed judge in the Go loop = 14 PASS / 0 FAIL.

### N-025 — planner check vs canonical plan thread

The old check depended on a historical local `.woody/planner-answer.json` and rejected approved legacy tier labels now mapped through the 0.11.3 model policy.

Acceptance:
- canonical PLAN + plan/N-* thread present;
- proposal grammar valid;
- existing PLAN remains non-overwritable;
- legacy tier aliases resolve through current policy;
- `check-planner.ps1` PASS.

## Update feed note

The standard signed updater feed requires the existing AirWorker Ed25519 private signing key. No matching AirWorker private key was found in the known SRVLM01 secret paths during this session. No new key was generated and the committed trust root was not changed.

This does not invalidate the GitHub Release or canonical GitHub marketplace path. Installation remains possible through the approved GitHub marketplace/plugin path, as used for 0.11.2.

## Remaining gates

- N-019 remains OPEN until a separate explicit LPR yes authorizes live installation and live readback/smoke.
- N-014 remains OPEN until its release/install child is accepted.
- N-004 remains OPEN until the AIR Commander r3.2 ChatGPT hook candidate is installed and live ChatGPT lifecycle acceptance passes.
- No live AirWorker installation was performed by this package acceptance.
