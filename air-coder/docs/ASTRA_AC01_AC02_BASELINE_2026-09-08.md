# Astra AC-01 / AC-02 baseline — 2026-09-08

## AC-01 — confirmed product passport

- Device: `SRVLM01` (`f8ff69f9-ff3c-465d-ba1d-309389ae1c93`).
- AIR OS root: `E:/-0-/AIR_OS`, local mode.
- Active startup: `E:/-0-/AIR_OS/00_BOOTSTRAP/AIR_START.md`, version `0.14`, status active.
- Mandatory Core: `E:/-1-/000_AI_Profile/00_ACTIVE_PROFILE/Core/Core_v4.1.4.md`, v4.1.4.
- Active development router observed now: `AIR_VIBECODING 2.0.0-rc.6`.
- 30-day rules digest: 272 total records; 57 in window; 6 open.
- Repository: `https://github.com/ruhorh66-rgb/air-plugins.git`.
- Canonical product path in repo: `E:/-7-/air-coder`.
- Active isolated development product root: `E:/-7-/_worktrees/aircoder-ruflo-contract-20260907/air-coder`.
- Branch: `ai/aircoder-ruflo-contract-beta3-20260907`.
- Observed branch HEAD before this passport artifact: `87c5541213a0c9d3f7c4e3022670be2c556459f4`; worktree was clean and synchronized with origin.
- Candidate product version: `0.1.0-beta.3`, release channel `development`.
- Entry skill: `air-coder/skills/route-coding-task/SKILL.md`.
- Main executable adapter: `air-coder/skills/route-coding-task/scripts/run_coding_task.py`.
- Runtime state root: `E:/-4-/air-coder/runs` by default; task/receipt state only, outside Git.

### Current executable environment

- Codex CLI: `0.145.0`, authenticated via ChatGPT in the observed SRVLM01 environment.
- Claude Code: `2.1.226`; retained as fallback, not a parallel default stack.
- Primary ready executor selected for this stage: Codex CLI.
- Substantial/swarm route remains the canonical Ruflo handoff; it is not the primary AC-03 executor.

### Open work at this passport point

- AC-03 full path implemented and one self-hosting task accepted.
- AC-04 five-task registry exists; 2/5 are accepted across `air-coder` and `air-ruflo-bridge`.
- Three replacement red-baseline tasks are preflighted for the next native quota window.
- Live interruption/resume mechanism check is prepared but not yet executed.
- Astra review, AC-04 acceptance threshold, AC-05 release package, merge/tag/install remain open.
- AirStorage and AIRVR are intentionally outside this stage until AirCoder release/acceptance.

## AC-02 — ready-executor adaptation map

| AIR behavior | Ready software capability | Thin AirCoder adaptation | Verification | Old-path condition |
|---|---|---|---|---|
| Execute one repo-local coding task | `codex exec --json -s workspace-write -C <repo>` | coding-task contract + `run_coding_task.py` | live Pilot-003 + independent diff/tests | ad-hoc direct coding invocation can be retired only after beta.3 release/install |
| Preserve executor session | Codex `thread_id` + `codex exec resume` | persist thread ID in runtime state | parser tests; live resume-check prepared | no AirCoder-owned conversation/session engine needed |
| Load exact product context | Codex working directory + explicit prompt files | context/repo/HEAD/origin gates before execution and resume | context/dirty/head-drift regressions | broad repo discovery is not a startup requirement |
| Bound writable scope | Codex workspace-write sandbox | allowed/protected paths + independent Git diff gate | protected/out-of-scope regressions | prompt-only scope protection is insufficient and is not trusted |
| Run repository checks automatically | normal shell/test commands outside the model | `run_acceptance()` takes commands from task contract and prepends `git diff --check` | accepted pilot receipts; full suites | user no longer reminds executor to run tests |
| Repair a reproducible failure | Codex resume on the same thread | bounded repair prompt with max 0..2 repairs | bounded-repair unit test; Pilot-002 honest limit | no custom planner/agent loop |
| Distinguish infra failure | Codex JSON error/turn events | `executor_unavailable` classification; no paid retry | live quota exhaustion + regression | repeated identical LLM retries are prohibited |
| Produce comparable result | Codex usage events + external checks | canonical run-result schema and persisted receipt | schema regression + AC-04 task registry | model self-report cannot declare ready |
| Route substantial work | existing Ruflo full-loop via bridge | selector + fail-closed Ruflo preflight | Ruflo component/runtime preflight | AirCoder does not become a swarm engine |

## Adaptation boundary

AirCoder remains `selector + thin ready-executor adapter`. It does not add a planner, scheduler, watchdog, daemon, universal gateway, learning database or substitute orchestration engine. Executor session ownership remains with Codex/Claude/Ruflo; AirCoder owns only the task contract, AIR-specific gates, bounded invocation policy and comparable receipts.

The installed beta.2 path is not disabled merely because beta.3 code exists. Switching the default small-task path to the new bounded runner is an AC-05 release/install decision after AC-04 acceptance and Astra review. Ruflo remains the substantial-development route; Claude Code remains fallback rather than a second default stack.
