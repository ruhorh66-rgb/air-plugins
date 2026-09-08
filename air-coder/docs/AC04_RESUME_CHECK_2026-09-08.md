# AC-04 live interruption/resume check — 2026-09-08

Status: prepared; native execution deferred until Codex quota is available.

Task: `AIRCODER-AC04-RESUME-001`
Product: `air-coder`
Isolated branch: `ai/aircoder-ac04-resume-taskid`
Red regression commit: `11540c62bcd9e554c76a6e9905fbd95451c2b544`
Execution baseline after resume-context hardening: `67a4dc6e596fab2f4a392613a97c30314463a372`
Runtime contract: `E:\-4-\air-coder\pilot-tasks\AIRCODER-AC04-RESUME-001.json`

## Real defect

`load_run()` currently accepts a resume `task_id` containing `../` before constructing the saved-state path. The protected regression proves that a crafted task can address a `state.json` outside `AIR_CODER_RUN_ROOT` instead of failing with `ContractError`.

## Execution protocol

1. Start the normal AirCoder runner once when native Codex quota is available.
2. Wait until the executor has completed and `state.status == executor_completed`.
3. The first acceptance command opens a deterministic 45-second `AC04_RESUME_WINDOW`.
4. During that window terminate only the outer runner process tree.
5. Record `thread_id`, `executor_attempts`, changed paths and state before restart.
6. Start a new process with the same task contract plus `--resume`.
7. Require acceptance to finish without another executor turn.

## PASS criteria

- first process performs exactly one executor turn;
- interruption occurs after that turn, not during `executor_running`;
- saved state remains recoverable and not `uncertain_inflight`;
- second process reuses the same canonical task and saved `thread_id`;
- `executor_attempts` remains `1` after resume;
- no second `codex exec` / `codex exec resume` turn is created;
- protected regression, full AirCoder suite and `git diff --check` pass after resume;
- final state is `accepted` with an independently verified diff;
- interruption and continuation require no SRVLM01 reboot.

## Prepared preflight

On SRVLM01 the task contract, context files, exact branch HEAD, origin, clean-start gate and configured limits all pass. Product code is intentionally still unfixed on the isolated branch so the execution remains a real coding task.

This mechanism check is separate from the five-task AC-04 acceptance registry and does not change its 2/5 current accepted count.
