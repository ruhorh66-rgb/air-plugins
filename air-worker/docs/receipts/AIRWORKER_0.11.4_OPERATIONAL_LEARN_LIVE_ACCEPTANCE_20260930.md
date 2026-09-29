# AirWorker 0.11.4 operational self-learning live acceptance — 2026-09-30

Node: SRVLM01
Plan node: N-028_0-11-4-operational-self-learning-procedures-for-s
LPR authorization: explicit yes to push, publish 0.11.4 and install the binary on the system.

## Release identity

Clean release lineage:
- functional source base: 5fe6d7ca05e91091f7d532d0d4b510e5f74bd34b
- release-boundary source: 3305c5516f25a15116dc221eaba284a46c3a29a4
- clean package commit/tag target: ba069b9aad710ad6663ab1c353bd5b93818c8a95
- tag: air-worker--v0.11.4
- GitHub Release: AirWorker 0.11.4
- repository: ruhorh66-rgb/air-plugins
- rollback tag: air-worker--v0.11.3

The local-only commits c901ec6/fac3b1e were NOT published. They were rejected from the release lineage because c901ec6 tracked private runtime LEARN state (approval/session/private ledger/review data). Origin did not contain them when detected.

Release guard:
- .air-worker/learn/ is ignored
- .campaign/ is ignored
- source ZIP contains zero .air-worker/learn, learn/proposals.jsonl, or .campaign entries

## Release assets

- air-worker-0.11.4-source.zip
  - bytes: 66,463,115
  - SHA-256: 51984D2FF6E4BC88A0C4E21993145B8104F90D23530C114082D7398B6D863695
- air-worker-0.11.4-windows-x64.exe
  - bytes: 9,040,384
  - SHA-256: D666383802D0569B5F3FF02D756CE0EE38DA2DB2F4B0766531A4806CD5B73FBE
- air-worker-tray-0.11.4-windows-x64.exe
  - bytes: 2,527,744
  - SHA-256: 427E7AF7E79C3CE038C436F306857FA18D8E9629EA3F96DC09B5EE8ED7B190AE

GitHub Release asset digests and sizes match the local artifacts exactly.

Package binary:
- version: 0.11.4
- embedded source revision: 3305c5516f25a15116dc221eaba284a46c3a29a4
- vcs.modified=false

## Self-learning delivered

Three LPR-approved, ledger-verified typed learned skills are active:

1. process-stall-diagnosis
   - proposal LP-20260929T154021Z-33fd0e0e
   - rule: when a process appears silent, inspect original PID, age, CPU, descendants and completed output before declaring a hang or retrying; do not duplicate heavy starts solely because polling has no new lines.

2. execution-unknown-no-blind-retry
   - proposal LP-20260929T154021Z-c883876c
   - rule: EXECUTION_UNKNOWN means unknown execution state, not failure; repeat start_process only after node/process/filesystem evidence proves the original command did not run.

3. file-lock-before-code-patch
   - proposal LP-20260929T154021Z-7b217ff8
   - rule: on Windows ACCESS_DENIED during replace/write, identify the file-handle owner before changing write-path code; supervised owners are released only through approved lifecycle control.

Generic verifier:
- check_type: gate
- check_spec: operational-procedure-v1
- verifier/testcases PASS
- learned rules are indexed skills, not hard-coded class branches.

Live installed AirWorker 0.11.4:
- air-worker learn context returns all three skills
- isolated live SessionStart returns additionalContext with all three skill classes
- process-stall-diagnosis_PRESENT=True
- execution-unknown-no-blind-retry_PRESENT=True
- file-lock-before-code-patch_PRESENT=True

## Regression

Source/package validation:
- Go test PASS
- go vet PASS
- plugin PASS
- Hermes 8 PASS / 0 FAIL
- ladder 10 PASS / 0 FAIL
- engine 14 PASS / 0 FAIL
- planner PASS
- binary PASS
- boundary PASS
- shell compatibility PASS
- reproducibility 43 PASS / 0 FAIL
- selftest PASS
- distribution identity PASS
- deploy PASS
- git diff --check PASS

The first optional distribution check invocation omitted its mandatory -Repo/-Version arguments and waited for PowerShell parameter input. It was stopped as our own stale check process and rerun correctly. This was not a product deadlock.

## Publication

Published:
- origin/main -> ba069b9aad710ad6663ab1c353bd5b93818c8a95
- origin/dev/airworker-0.11.2-gpt-learn-lifecycle -> ba069b9aad710ad6663ab1c353bd5b93818c8a95 at package publication
- origin/release/air-worker-0.11.4-clean-20260930 -> package lineage
- annotated tag air-worker--v0.11.4 -> ba069b9aad710ad6663ab1c353bd5b93818c8a95
- GitHub Release AirWorker 0.11.4 with three verified assets

## Live installation on SRVLM01

Pre-install:
- live AirWorker 0.11.3
- CLI SHA-256: 35D285BDF0464DEBE5DDB303FC9C88658482E1695C5B654D9B722A12E3324C53
- tray SHA-256: AE9697E3B9BE10AE18C96C096A217C530A2D70AAC2CD81122A564782C23C6F33

Canonical marketplace update:
- Claude marketplace: github ruhorh66-rgb/air-plugins
- Claude air-worker plugin: 0.11.3 -> 0.11.4
- Codex marketplace: github ruhorh66-rgb/air-plugins
- Codex air-worker plugin: 0.11.4
- canonical Claude cache binary SHA matched release asset before install

Install source:
R:/-4-/claude-home/plugins/cache/air-plugins/air-worker/0.11.4/bin/air-worker.exe

Install command:
- air-worker install -no-start
- no UAC
- no reboot
- no foreign service stop
- tray restarted afterward only with air-worker tray -ensure

Post-install:
- live version: air-worker 0.11.4
- live revision: 3305c5516f25a15116dc221eaba284a46c3a29a4
- live vcs.modified=false
- live CLI SHA-256: D666383802D0569B5F3FF02D756CE0EE38DA2DB2F4B0766531A4806CD5B73FBE
- live tray SHA-256: 427E7AF7E79C3CE038C436F306857FA18D8E9629EA3F96DC09B5EE8ED7B190AE
- tray running
- Claude active profile: 0.11.4 / canonical GitHub / matching revision and payload
- Codex active profile: 0.11.4 / canonical GitHub / matching revision and payload
- install -status: no source violations
- selfcheck: warnings=[] / violations=[] / not_proven=[]

## Rollback

Rollback target:
- tag air-worker--v0.11.3
- GitHub marketplace source ruhorh66-rgb/air-plugins
- CLI SHA-256 35D285BDF0464DEBE5DDB303FC9C88658482E1695C5B654D9B722A12E3324C53
- tray SHA-256 AE9697E3B9BE10AE18C96C096A217C530A2D70AAC2CD81122A564782C23C6F33

Rollback remains via official GitHub marketplace/tag path; do not edit plugin registries by hand.

## Result

PASS

AirWorker 0.11.4 is published and live on SRVLM01. N-028 done_when is satisfied.
