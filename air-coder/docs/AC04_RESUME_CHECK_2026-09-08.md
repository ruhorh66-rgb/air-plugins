# AC-04 live interruption/resume check ? 2026-09-08

Status: PASS.

Task: `AIRCODER-AC04-RESUME-001`
Initial SHA: `67a4dc6e596fab2f4a392613a97c30314463a372`
Exact native accepted diff SHA: `86caeadd61e4c5b60257ecbe2faa2205ee83eb71`
Candidate hardening SHA: `4b3df94` plus later schema/runtime hardening.
Runtime state: `E:/-4-/air-coder/runs/AIRCODER-AC04-RESUME-001/state.json`

## Executed proof

- First process reached persisted `executor_completed` after exactly one native executor turn.
- Only the outer AirCoder runner process tree was terminated; the executor was not interrupted in-flight.
- A new process resumed the same task with `--resume`.
- Final status: `accepted`; acceptance: 4/4.
- `executor_attempts`: 1 before interruption and 1 after resume.
- `executor_runs`: 1 total; no second Codex turn was created.
- Thread preserved: `01a08194-1b39-7052-834a-edad546d7b49`.
- `resume_count=1`; active elapsed `446.076 s`; resume wait `0.457 s`.
- Changed path: only `air-coder/skills/route-coding-task/scripts/run_coding_task.py`.
- During the run RDC transport briefly disconnected; no task replay was triggered. The same run continued when transport returned.

Evidence: `E:/-4-/air-coder/live-resume-evidence/AIRCODER-AC04-RESUME-001-summary.json` plus first/resume logs and canonical state receipt.

This mechanism check is separate from the five-task AC-04 counter. AC-04 itself is now 4/5 accepted with false-ready=0.
