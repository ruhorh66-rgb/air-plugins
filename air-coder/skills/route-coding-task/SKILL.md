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

Prepare a small JSON object for `scripts/select_executor.py`:

- `mode`: `analysis` or `implementation`;
- `size`: `small`, `medium`, `large`;
- `substantial_signs`: any of `new_subsystem`, `new_integration`, `migration`, `wide_release`, `cross_repo`, `architecture_change`, `parallel_roles`;
- `scarce_quota_policy`: `conserve`, `balanced`, `speed`;
- `repo_hands_priority`: `normal` or `high`;
- `native_preference`: `auto`, `codex`, `claude`;
- optional `lpr_route`: explicit LPR override.

## Selection rules

1. `analysis` → `chatgpt_rdc`: use the strong reasoning channel without spending scarce native CLI quota.
2. substantial implementation or `size=large` → `ruflo`: substantial work is where Ruflo overhead is justified.
3. otherwise, when fast repository hands are high priority and quota policy is not `conserve` → `native_cli`.
4. all other small/local work → `chatgpt_rdc`.
5. explicit `lpr_route` wins over the selector.

Run:

```powershell
python skills/route-coding-task/scripts/select_executor.py --input task.json --pretty
```

## Mandatory live probe

A route decision is not runtime evidence. Before execution:

- `chatgpt_rdc`: verify the RDC connector is callable;
- `native_cli`: resolve and smoke the selected `codex` or `claude` CLI;
- `ruflo`: run `scripts/probe_ruflo_route.py --pretty` and require `status=PASS` before any Ruflo launch.

For `ruflo`, AirCoder owns the handoff contract but not swarm mechanics. The only execution path is `air-ruflo-bridge:run-via-ruflo` / `run_task.ps1`. First call the bridge without `-Approval` to obtain the canonical dry-run/proposal. Inspect objective delivery and proposal evidence. Then use an already-explicit LPR approval for that exact objective, or request approval once, and execute through the same bridge.

AirCoder must never replace the bridge with direct `claude -p`, manual `swarm_init/task_create/agent_execute`, or direct hive-mind CLI orchestration. A failed Ruflo preflight is fail-closed: report the failed check and do not improvise another Ruflo launcher.

Ruflo component truth is `contracts/ruflo-route-profile.json`. Full CLI loop capabilities are mandatory; individual upstream plugins are task-triggered unless the profile marks them required. Re-check the upstream component catalog before every engine upgrade.

If the selected route is unavailable, report the failed probe and re-run selection only with the changed availability/economics facts or an explicit LPR override. Do not invent a substitute executor inside AirCoder.

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
