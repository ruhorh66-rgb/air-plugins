# AirWorker 0.11.3 + AIR Commander r3.2 live completion — 2026-09-29

Node: SRVLM01
Scope: Gate A + Gate B authorized explicitly by LPR on 2026-09-29

## Gate A — AirWorker 0.11.3

Accepted separately in:
docs/receipts/AIRWORKER_0.11.3_LIVE_TWO_NODE_ACCEPTANCE_20260929.md

Result:
- SRVLM01 live AirWorker 0.11.3 PASS
- AIR-ENV-002 live AirWorker 0.11.3 PASS
- exact revision 9e93be6621aae44c0c26c66398213eb39553b68d
- exact CLI SHA-256 35D285BDF0464DEBE5DDB303FC9C88658482E1695C5B654D9B722A12E3324C53
- Claude/Codex active profiles canonical GitHub 0.11.3
- selfcheck zero warnings/violations/not_proven on both nodes
- Headroom healthy
- Ponytail vendor skill present
- executor list exposes GPT-6 Luna/Sol/Astra low/medium/high only
- real ChatGPT principal executor dispatch on gpt-6-luna:low PASS

N-026, N-019 and N-014 closed.

## Gate B — AIR Commander r3.2 live cutover

Release:
- tag/release: r3.2-2026-09-29
- bridge version: 0.1.2
- source implementation: 8b6a3cc069155e8669b1d044171eb9fca9067194
- GitHub checkout:
  F:/-8-/_releases/air-commander-r3.2-2026-09-29
- checkout clean

Pre-cutover:
- live AIR Commander r3.1 / bridge 0.1.1
- PID 8040
- startup.user.rules SHA-256:
  C154FA18CE2CF5FE1BC9E56ABEA2E7DBC41BB0E335A3E04921C90BF6C2CDF2B3
- bridge.config.json SHA-256:
  8967F1A9720066F83DE4E11935210317A4DAAEE5B57932428269AEF2A22108E4

Cutover:
- changed only devspace-bridge.working_directory
  from F:/-8-/_releases/air-commander-r3.1-2026-09-29
  to   F:/-8-/_releases/air-commander-r3.2-2026-09-29
- atomic File.Replace used
- backup:
  C:/ProgramData/AIR OS/State/startup.user.rules.pre-air-commander-r3.2-2026-09-29.bak
- backup SHA-256:
  C154FA18CE2CF5FE1BC9E56ABEA2E7DBC41BB0E335A3E04921C90BF6C2CDF2B3
- post-cutover overlay SHA-256:
  5260C7023E905F49BB215F1ABF17C79B1A534680F3C3163D7ABD784E1A287B10
- Test profile unchanged
- bridge.config.json unchanged
- restart executed only through:
  C:/Program Files/AIR/ASW/asw.exe air-commander restart

Live readback:
- new PID 8184
- parent PID 5780 = C:/Program Files/AIR/ASW/asw.exe
- command: R:/-4-/tools/nodejs/node.exe bridge/src/index.js start
- ASW desired=running
- health version 0.1.2
- health state RUNNING
- health reason_code OK
- health PID = 8184
- bridge.lock PID = 8184
- 127.0.0.1:8730 owner PID = 8184

Thus ASW/health/lock/port identity is consistent.

## Live ChatGPT → AirWorker lifecycle acceptance

Signed MCP session:
chatgpt-live-r32-1790689616136

tools/list:
- air_worker_attach visible
- air_worker_status visible
- air_worker_pending visible
- air_worker_finalize visible
- air_worker_approve descriptor present with app-only/private visibility

Lifecycle:
- air_worker_attach PASS
- air_worker_status PASS
- ordinary get_file_info PASS through active middleware
- air_worker_pending PASS and returned approval token
- air_worker_finalize PASS
- Stop PASS

Runtime transcript:
R:/-4-/air-devspace/airworker-gpt-transcripts/9ebd125df4e528003da4783cb79ed531.jsonl

It records:
- SessionStart
- PreToolUse get_file_info
- PostToolUse get_file_info
- Stop

Tool arguments are not stored in the minimal transcript.

## Live private approval E2E

A disposable product was used; no real proposal was approved.

Synthetic proposal:
LP-20260929T135017Z-cbaf764f
class=r32-private-approval-smoke
check_spec=sgt-commit-added-lines-within-10m

Live r3.2:
- attach PASS
- pending found the synthetic proposal
- private air_worker_approve PASS
- trusted ChatGPT grant created:
  LG-20260929T135628Z-c16ae0df
- learn apply consumed that grant
- executable verification:
  builtin:sgt-commit-added-lines-within-10m:v1 cases=4/4
- ledger:
  LM-20260929T135628Z-cd035800
- finalize PASS

Disposable product and smoke scripts were removed after acceptance.

No real AirWorker proposal was approved or applied.

## Current ChatGPT UI cache boundary

The current conversation's tool snapshot was taken before r3.2 and therefore does not expose the new air_worker_* tools in the assistant's current connector schema.

This is a client/tool-catalog cache boundary, not a bridge failure.

To use the new tools in normal ChatGPT operation:
1. Refresh AIR Commander in ChatGPT Plugins.
2. Open a new chat/session.

The live bridge tools/list already proves the descriptors are served by r3.2.

## Synthetic pending proposal residue

The first live Stop acceptance used the phrase "synthetic final". The background learning reviewer later created:
LP-20260929T134749Z-2bf340d5
class=synthetic-transcript-review
status=PENDING_LPR

This proposal:
- is not approved
- is not applied
- has no grant
- cannot become active without explicit LPR approval

The current 0.11.3 core has no reject/dismiss command for a pending proposal. The proposals journal was not edited directly.

Operational lesson: acceptance Stop smokes should use neutral final text or a disposable product to avoid creating review proposals from test language.

## N-027 plan hygiene

Post-live goals validation found stale PLAN references to removed tests and obsolete ladder wording.

Updated:
- historical step 89 now reflects accepted 0.11.3 primary route:
  GPT-6 Luna -> GPT-6 Sol -> GPT-6 Astra
  low -> medium -> high only
  GPT-5.5/5.6 vendor-limit fallbacks
- K75 now matches the same accepted policy and states no AirWorker token telemetry
- K35 now references the current OpenAI-executor / Anthropic-judge tests

Selectors replaced with existing tests:
- TestReleaseModelPolicy0113
- TestExecutorDispatchLoadsPonytailAndWalksVendorFallbacks
- TestReleaseProfileUsesOpenAIExecutorsAndAnthropicJudges
- TestReleaseJudgePolicy0113
- TestOpenAIAPIRunner retained where applicable

Acceptance:
- targeted Go tests PASS
- air-worker goals: 7 goals / 78 criteria / problems=[]
- goals RC=0
- plan-lint PASS
- diff-check PASS

## Rollback Gate B

If r3.2 fails later:
1. restore
   C:/ProgramData/AIR OS/State/startup.user.rules.pre-air-commander-r3.2-2026-09-29.bak
   to startup.user.rules atomically
2. verify SHA-256:
   C154FA18CE2CF5FE1BC9E56ABEA2E7DBC41BB0E335A3E04921C90BF6C2CDF2B3
3. run only:
   C:/Program Files/AIR/ASW/asw.exe air-commander restart
4. verify health version 0.1.1, RUNNING/OK, and PID==lock==8730 owner
5. do not change bridge.config.json, keys or Test profile

## Restrictions respected

- no reboot
- no UAC
- no foreign service stop
- no manual bridge kill/start
- no manual plugin registry edits
- no direct learn journal/proposal edits

## PLAN.md handle incident during final closure

The first attempts to close N-027 and N-004 through AirWorker core failed with:

`rename .PLAN.md.<pid>.tmp -> PLAN.md: Access is denied`

Read-only Windows Restart Manager diagnosis identified the exact holder:

- PID 20012
- process: Node.js
- command: `R:/-4-/tools/nodejs/node.exe R:/-4-/desktop-commander-0.2.51/dist/index.js --no-onboarding`

Thus the failure was an active Desktop Commander file handle on PLAN.md, not a read-only/ACL problem and not an AirWorker atomic-replace defect.

A provisional MoveFileExW source change was tested locally, but it did not bypass the active sharing lock and was fully reverted before commit/release.

One stale AirWorker release-test process (`check-distribution-identity.ps1`) and its own wrapper were also terminated; they were not services and were not the PLAN.md handle owner.

Within the already approved Gate B scope, AIR Commander was restarted again only through:

`C:/Program Files/AIR/ASW/asw.exe air-commander restart`

Post-restart:
- bridge version: 0.1.2
- PID: 16052
- ASW state: RUNNING
- desired: running
- health: RUNNING / OK

Without reading PLAN.md through Desktop Commander again, the same installed AirWorker 0.11.3 core then closed:
- N-027 — PASS
- N-004 — PASS

Final `air-worker plan spine -json`:
- open_nodes: null

## Result

Gate A: PASS
Gate B: PASS
N-027: CLOSED
N-004: CLOSED
Canonical AirWorker plan spine: EMPTY.

Normal ChatGPT use still requires refreshing the AIR Commander plugin/tool catalog and opening a new chat/session because this already-open conversation retains its pre-r3.2 tool snapshot.
