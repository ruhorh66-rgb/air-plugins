# AirCoder / Ruflo modernization handoff — 2026-09-07

status: LPR_APPROVED_SEQUENCE

## Решение ЛПР

Порядок работ фиксируется жёстко:

1. Сначала модернизировать AirCoder на полном актуальном наборе Ruflo capabilities.
2. Выпустить и установить новый рабочий релиз AirCoder.
3. Только затем использовать обновлённый AirCoder для модернизации AirStorage.
4. После модернизации AirStorage вернуться к AIRVR / AIR Copilot.

Не начинать модернизацию AirStorage раньше готового AirCoder release.

## Текущий AirCoder candidate

- branch: `ai/aircoder-ruflo-contract-beta3-20260907`
- checkpoint: `5eb34c92932b2e33b9e0f9674a46d6d372657227`
- target version: `0.1.0-beta.3`
- tests: 11/11 PASS
- Ruflo route preflight: PASS
- Claude manifests: PASS
- component matrix: `docs/RUFLO_35_COMPONENT_ASSIMILATION_2026-09-07.md`
- route profile: `contracts/ruflo-route-profile.json`
## Ruflo runtime state

- current engine pin: `3.38.21`
- rollback: `3.38.11`
- `.mcp.json` points to the 3.38.21 CLI cache
- Agent Booster / `agentic-flow` link verified
- no Ruflo swarm was started after the LPR instruction to stop until protocol verification completed
- yesterday's successful full-loop launch remains the reference launch pattern

## Required AirCoder modernization wave

Process all 35 upstream Ruflo plugins/capabilities. Do not pre-reject a component.
For every component record one implementation verdict:

- `ADOPT_RUNTIME` — use upstream component/runtime directly;
- `MERGE` — retire overlapping AIR code into upstream capability;
- `WRAP` — retain only AIR-specific policy/contract around upstream;
- `PATTERN_ADOPT` — reuse the mechanism/pattern where runtime embedding is not appropriate.

Priority AirCoder families: core, swarm, autopilot, workflows, loop-workers, goals,
intelligence, DAA, testgen, browser, jujutsu, security/aidefence, observability,
cost-tracker, migrations, MetaHarness/Arena and coding-oriented graph/memory surfaces.
## Definition of done before AirStorage starts

AirCoder modernization is complete only after:

1. 35/35 component decisions are converted from matrix entries into executable integration work.
2. Canonical Ruflo handoff uses only the documented bridge / full CLI loop.
3. Direct `claude -p` and manual MCP/swarm orchestration are mechanically blocked for the Ruflo route.
4. Dry-run / objective-delivery / approval / execution sequence is covered by tests or mechanical gates.
5. AirCoder release candidate passes host delivery in Claude Code and Codex.
6. Commit, push, merge, tag and release are complete.
7. Installed release is smoke-tested before AirStorage work begins.

## Next session first action

Resume from this branch. Do not reopen AIR OS or AIRVR architecture. Finish AirCoder
Ruflo assimilation and release first. Then route the AirStorage modernization task
through that released AirCoder and consume the saved AirStorage 35-component matrix.
