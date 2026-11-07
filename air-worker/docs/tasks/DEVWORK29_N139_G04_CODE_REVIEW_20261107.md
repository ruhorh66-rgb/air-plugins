# N139/G04/G05 · independent Anthropic source acceptance of one autonomous AirWorker fix
TASK ID: DEVWORK29_N139_G04_CODE_REVIEW_20261107
NATIVE_NODE_ID: N-139_n136-native-operational-goal-selector-for-real-a
OWNER: Dev Work 29. TARGET: AirWorker source-only candidate, NOT installed product.
SOURCE FILE TO READ FIRST: docs/tasks/DEVWORK29_N139_G04_CODE_REVIEW_20261107.md
READ-ONLY TOOLS: Read, Glob, Grep. Do NOT use Bash, shell, or command execution. Native CLI supplies SHA256 of THIS task and exact Git HEAD. If Read cannot open this actual file, report TASK_READ_FAIL and NOT_PROVEN; repeating inline prompt contents does not prove task access. Do NOT search for generic TASK.md.

## Verified context (do not accept executor claims in place of facts)
ONE native air-worker loop selected full N139; Codex Luna low edited cmd/job_receipt.go and cmd/job_status.go and terminal DONE job id gpt__devwork29-7fc2da058a095e9a__1794026842523903500__3115cf12.
Native factual Go judge job DONE: gpt__devwork29-7fc2da058a095e9a__1794027935412442500__30cad5a6. Actual output ok airos/air-worker 23.993s, SHA256 94efe696195b25b8e932f1dba57b71b15bc296e4524de8d536c3fea8fdbe652c.
Original Anthropic semantic verdict NOT_PROVEN, not PASS: the Codex narrative referenced a different stalled manual judge; factual native Go had already completed RC0. Do NOT revise the old verdict; independently review this frozen source.
Initial RED TestN139TerminalOutputIntegrityRejectsTamper RC1; after change targeted Go tests GREEN RC0 and Go vet RC0 independently reproduced. Fix seals terminal output SHA+bytes in job_receipt.go and checks readback in job_status.go.
Baseline HEAD 8c813f673c4d3ccae472f441183f78903fe6efb8. Source-only candidate commit HEAD supplied by the native review CLI. Installed AirWorker 0.11.11-rc.2 unchanged. Historical release-slices 0.11.9 SHA preserved. N139/N136/N126/N123 OPEN.

## Actual review tasks
First Read docs/tasks/DEVWORK29_N139_G04_CODE_REVIEW_20261107.md using Read. Then Read cmd/job_receipt.go, cmd/job_status.go, cmd/operational_run.go, cmd/operational_run_test.go, cmd/job_status_test.go and cmd/job_receipt_test.go. Use exact line citations with small code quotes.
Check: sealing only after terminal process completion, changed output digest/size refusal; historical unsealed receipts must not be represented as verified.
Check: no false positive on Go RC fail, unknown job status, missing task or independent reviewer NOT_PROVEN; N139 must not close just from model DONE.
Check: concurrent session isolation, symlink/foreign file handling and bounded output. Identify any P0/P1 in this scope with exact line references.
Review negative tests and original RED→GREEN evidence without treating source tests as whole P0 acceptance. Do not require a full suite if targeted source slice suffices; mention broader N123 full QA still open.
Use EXACT native machine JSON schema. task_read=TASK_READ_OK only if you actually Read this file. PASS_SOURCE_ONLY only if safe and zero findings with >=3 distinct code file citations. FAIL_P0/FAIL_P1 with sourced finding if unsafe, NOT_PROVEN if unreadable. Do not install or edit files.
