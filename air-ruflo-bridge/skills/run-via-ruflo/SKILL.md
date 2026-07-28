---
name: run-via-ruflo
description: Run a coding or shell task through an installed Ruflo/claude-flow runtime while enforcing a mandatory human approval gate before terminal execution and independently checking daemon state. Use when a user asks to orchestrate work with Ruflo, initialize a Ruflo swarm, or verify its daemon without waking it.
---

# Run via Ruflo

Use the real Ruflo CLI; do not implement a swarm or orchestration substitute.

## Install only when needed

Run `scripts/install_ruflo.ps1`. It detects an existing `.claude-flow` installation or cached CLI and exits without changing it. For a fresh project, provide a trusted cached `claude-flow/bin/cli.js`; it runs the piloted non-interactive initializer. Do not alter the pilot at `E:\-4-\ruflo-pilot`.

## Mandatory human gate

First run `scripts/run_task.ps1` **without** `-Approval`. It may call only `swarm_init` and `task_create`, writes the proposed topology, role assignments, task descriptions, and budget (`maxAgents`) to its report, then stops with exit code 10.

Show the complete proposal/report to the human and wait for an explicit approval. Do not infer approval from silence or from a prior request. Only after approval, re-run with the literal `-Approval I_APPROVE_RUFLO_PLAN` and an explicit `-Command`. The script is the only path here that calls `terminal_execute`; it refuses to call it before the gate.

Never say roles were actually distributed merely because Ruflo proposed them or `task_create` succeeded. Require evidence of distinct Ruflo agent IDs and distinct OS process IDs/calls before making that claim; otherwise say only “roles proposed”. Likewise, do not report a successful run solely from Ruflo output—verify the requested artifact/test independently.

## Daemon status without waking it

Do not call Ruflo's daemon-status command for a simple status check: it can start workers. Run `scripts/verify_daemon_state.ps1` instead. It reads the state file and compares its PID to `Get-CimInstance Win32_Process`; it reports exactly one of `confirmed`, `contradicted`, or `unverifiable`. Treat `unverifiable` as unknown, not stopped.
