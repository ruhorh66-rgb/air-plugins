# AirWorker 0.11.3 live installation acceptance — 2026-09-29

Node set: SRVLM01 + AIR-ENV-002
Authorized by LPR: explicit yes for Gate A on 2026-09-29
Live release: AirWorker 0.11.3
Rollback target: air-worker--v0.11.2

## SRVLM01

Live readback:
- version: 0.11.3
- revision: 9e93be6621aae44c0c26c66398213eb39553b68d
- CLI SHA-256: 35D285BDF0464DEBE5DDB303FC9C88658482E1695C5B654D9B722A12E3324C53
- Claude GitHub plugin: 0.11.3
- Codex GitHub plugin: 0.11.3
- payload SHA-256 matches between hosts
- selfcheck: warnings=0, violations=0, not_proven=0
- tray running

Live executor acceptance:
- executor list exposed the approved primary ladder:
  - gpt-6-luna low/medium/high
  - gpt-6-sol low/medium/high
  - gpt-6-astra low/medium/high
- no max/ultra in the primary executor ladder
- approved 5.5/5.6 fallback pools present
- Headroom http://localhost:8787 health: ready=true, upstream healthy
- installed Ponytail vendor skill present
- ChatGPT session declaration PASS
- real executor dispatch:
  - principal: chatgpt
  - tier: gpt6-luna:low
  - model: gpt-6-luna
  - result: success
  - result text included EXECUTOR_SMOKE_OK
  - durable receipt created
- temporary task file removed and session forgotten

Two earlier smoke setup attempts were correctly rejected before model execution:
- unsupported -json on session declare
- missing PLAN.md in a disposable product
They are not counted as product failures.

## AIR-ENV-002

Preinstall drift observed:
- R:/-4-/claude-home registry: 0.11.1
- C:/Users/admin_loc/.claude registry: 0.10.9
- user Codex cache: 0.11.2

Repair used only official plugin CLIs. No registry JSON was edited by hand.

Claude:
- R:/-4-/claude-home marketplace updated from canonical GitHub
- R:-Claude AirWorker updated 0.11.1 -> 0.11.3
- user-Claude marketplace updated from canonical GitHub
- user-Claude AirWorker updated 0.10.9 -> 0.11.3
- "already enabled" responses were treated as idempotent state, not an error

Codex:
- PowerShell .ps1 wrapper was blocked by the node's execution policy
- switched to the official codex.cmd launcher; no policy bypass was used
- R:/-4-/codex-home marketplace upgraded, AirWorker installed 0.11.3
- C:/Users/admin_loc/.codex marketplace upgraded, AirWorker installed 0.11.3

Live install:
- source: C:/Users/admin_loc/.claude/plugins/cache/air-plugins/air-worker/0.11.3/bin/air-worker.exe
- source verified as canonical GitHub marketplace cache
- source version: 0.11.3
- source SHA-256: 35D285BDF0464DEBE5DDB303FC9C88658482E1695C5B654D9B722A12E3324C53
- installer stopped old tray, copied CLI/tray, verified version and SHA
- install used -no-start, then tray -ensure
- live version: 0.11.3

AIR-ENV-002 selfcheck:
- live revision: 9e93be6621aae44c0c26c66398213eb39553b68d
- live SHA-256: 35D285BDF0464DEBE5DDB303FC9C88658482E1695C5B654D9B722A12E3324C53
- active user-Claude: 0.11.3 canonical GitHub
- active user-Codex: 0.11.3 canonical GitHub
- payload SHA-256 match
- warnings=0
- violations=0
- not_proven=0

## Rollback proof

Exact 0.11.2 GitHub rollback was proved in disposable profiles before live installation.

Claude:
- marketplace source ruhorh66-rgb/air-plugins@air-worker--v0.11.2
- plugin installed 0.11.2
- git commit d33424d089fd8576980be5eb09af9eb4d36a1751

Codex:
- marketplace source ruhorh66-rgb/air-plugins
- ref air-worker--v0.11.2
- plugin installed 0.11.2

Disposable rollback profiles were removed.

## Restrictions respected

- no UAC
- no reboot
- no foreign service stop
- no manual plugin registry edits
- no manual cache copying
- install source remained canonical GitHub marketplace cache

## Result

PASS

N-026 may close.
N-019 may close.
N-014 may close after N-019 closure.
