# AirWorker 0.11.3 N-020 Ponytail task-consumption fix acceptance — 2026-09-29

Node: SRVLM01
Plan node: N-020_0-11-3-ponytail-noninteractive-task-consumption-f
Source commits: 8fe2886, 4ec80c7

## Finding

Two first N-018 live smokes returned Codex success and Ponytail activation text but did not execute the authoritative task-file; the disposable target remained unchanged.

The first suspected `@ponytail full` marker was removed. Follow-up proved the deeper transport fault: multiline Ponytail+task payload was passed through Windows `codex.cmd` as argv. The installed Ponytail skill is only 6757 bytes, so this was not a prompt-size budget issue.

## Accepted fix

- AirWorker still loads the exact installed vendor Ponytail SKILL.md and marks ACTIVE MODE: full.
- Non-interactive Codex prompt payload uses stdin, not a multiline argv.
- The real coding smoke after the fix read the task-file, edited only the disposable target and returned `GPT_EXECUTOR_OK`.
- Regression and vet PASS; source is committed and pushed.
