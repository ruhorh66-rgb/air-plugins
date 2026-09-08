# AirCoder

AirCoder is a thin AIR adapter around existing coding executors. It keeps the user-facing task entrypoint in chat and avoids building another planner, agent loop or scheduler.

```text
chat task
  -> AirCoder task/context contract
  -> executor selection
  -> ready executor
     |-- Codex CLI (primary bounded coding path)
     |-- Claude Code fallback
     `-- Ruflo via air-ruflo-bridge for substantial/swarm work
  -> independent diff + repository checks
  -> bounded repair
  -> persisted result
```

## Select an executor

Create `task.json` with task/economics facts and run:

```powershell
python skills/route-coding-task/scripts/select_executor.py --input task.json --pretty
```

Selection is not availability proof; live-probe the chosen route.

## Run a bounded Codex coding task

Create a task contract matching `contracts/coding-task.schema.json`, then run:

```powershell
python skills/route-coding-task/scripts/run_coding_task.py --task <task.json>
```

The runner loads required context, checks repository identity/clean start, invokes the existing Codex CLI, independently checks changed/protected paths and repository acceptance commands, and allows at most two repair turns.

Runtime state and receipts are stored outside Git under `AIR_CODER_RUN_ROOT` (default `E:/-4-/air-coder/runs`). To continue a saved non-inflight task:

```powershell
python skills/route-coding-task/scripts/run_coding_task.py --task <task.json> --resume
```

An interrupted `executor_running`/`repair_running` state fails closed as `uncertain_inflight`; AirCoder does not replay that paid call automatically. Codex never owns commit/push/merge/tag/release in this bounded path.