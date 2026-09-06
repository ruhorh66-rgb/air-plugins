# AirCoder

AirCoder is a thin executor selector for AIR coding work.

It answers one question: **which existing execution path should handle this task, and why?**

```text
AirCoder
  ├─ ChatGPT + RDC
  ├─ Ruflo via air-ruflo-bridge
  └─ native Codex / Claude Code
```

AirCoder does not implement its own swarm, controller, scheduler, watchdog, queue, learning store, model gateway, or provider transport.

## Quick start

Create `task.json` with task/economics facts, then run:

```powershell
python skills/route-coding-task/scripts/select_executor.py --input task.json --pretty
```

Before executing the selected route, perform its live availability probe. After execution, record the result using `contracts/run-result.schema.json`.
