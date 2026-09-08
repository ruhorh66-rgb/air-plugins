---
name: route-coding-task
description: Select the cheapest suitable executor for an AIR coding task and hand off to the existing ChatGPT+RDC, Ruflo, or native Codex/Claude route.
---

# AirCoder — route coding task

AirCoder is a selector, not an orchestration engine. It must not implement a swarm, scheduler, watchdog, LLM gateway, queue, or learning store.

## Read first

1. `product.json`
2. `00_STRATEGY/AIR_CODER_LIVING_DOCUMENT.md`
3. `docs/GOAL.md` for the active release cycle
4. `capabilities.json`

## Input facts

Infer a small selector payload from the user's ordinary chat request. Do not ask the user to choose a route or prepare JSON when repository/context facts are already available. The internal payload for `scripts/select_executor.py` uses:

- `mode`: `analysis` or `implementation`;
- `size`: `small`, `medium`, `large`;
- `substantial_signs`: any of `new_subsystem`, `new_integration`, `migration`, `wide_release`, `cross_repo`, `architecture_change`, `parallel_roles`;
- `scarce_quota_policy`: `conserve`, `balanced`, `speed`;
- `repo_hands_priority`: `normal` or `high`;
- `native_preference`: `auto`, `codex`, `claude`;
- optional `lpr_route`: explicit LPR override.

## Selection rules

1. explicit `lpr_route` wins over the selector.
2. `analysis` → `chatgpt_rdc`: keep reasoning in the normal control channel.
3. implementation with explicit `substantial_signs` → `ruflo`; size alone must not force Ruflo.
4. ordinary implementation → `native_cli`, with Codex as the default ready executor for this release cycle.
5. explicit `scarce_quota_policy=conserve` may keep implementation in `chatgpt_rdc`.
6. if the selected native executor is quota-blocked/unavailable, return the real unavailable/waiting status; do not silently fall back to a paid API or force Ruflo.

The session may materialize this payload as an internal task-facts file and run the selector itself. Internal routing JSON is implementation detail, not a user-facing prerequisite.

## Mandatory live probe

A route decision is not runtime evidence. Before execution:

- `chatgpt_rdc`: verify the RDC connector is callable;
- `native_cli`: resolve and smoke the selected `codex` or `claude` CLI;
- `ruflo`: run `scripts/probe_ruflo_route.py --pretty` and require `status=PASS` before any Ruflo launch.

For `ruflo`, AirCoder owns the handoff contract but not swarm mechanics. The only execution path is `air-ruflo-bridge:run-via-ruflo` / `run_task.ps1`. First call the bridge without `-Approval` to obtain the canonical dry-run/proposal. Inspect objective delivery and proposal evidence. Then use an already-explicit LPR approval for that exact objective, or request approval once, and execute through the same bridge.

AirCoder must never replace the bridge with direct `claude -p`, manual `swarm_init/task_create/agent_execute`, or direct hive-mind CLI orchestration. A failed Ruflo preflight is fail-closed: report the failed check and do not improvise another Ruflo launcher.

Ruflo component truth is `contracts/ruflo-route-profile.json`. Full CLI loop capabilities are mandatory; individual upstream plugins are task-triggered unless the profile marks them required. Re-check the upstream component catalog before every engine upgrade.

If the selected route is unavailable, report the failed probe and re-run selection only with the changed availability/economics facts or an explicit LPR override. Do not invent a substitute executor inside AirCoder.

## Ready-agent execution (Astra AC-03)

When the chosen implementation path is the ready Codex agent, do not hand the user internal CLI commands. Build a `contracts/coding-task.schema.json` task contract and run:

```powershell
python skills/route-coding-task/scripts/run_coding_task.py --task <task.json>
```

The runner owns only the thin adaptation layer: context/repository gate, Codex `exec/resume`, changed/protected path gate, repository checks, at most two repair attempts, and a persisted receipt. Runtime state goes to `AIR_CODER_RUN_ROOT` (default `E:/-4-/air-coder/runs`).

A saved task is resumed with the same contract plus `--resume`. If the previous state is `executor_running` or `repair_running`, the runner returns `uncertain_inflight` and MUST NOT replay the paid call automatically.

Codex must not commit, push, merge, tag or release from this path. Those remain outer AIR development/release stages.

## Result contract

Every real execution records one result matching `contracts/run-result.schema.json`:

- acceptance `N/M`;
- elapsed minutes;
- direct monetary cost, or `null` when not measurable;
- scarce quota burden: `low`, `medium`, `high`;
- actual model/executor class;
- attempts/retries;
- evidence paths or receipts.

The comparison target is cost per verified result, not model price per hour.

## Boundaries

- Existing executors remain owners of their execution mechanics.
- AirCoder does not modify `air-worker`, `air-ruflo-bridge`, Codex, Claude Code, RDC, or AIR LLM Router.
- A configured provider is not proof of the effective executor; record the actual model/runtime in the result.
- No release, merge, or irreversible external action is performed merely because AirCoder selected a route.
