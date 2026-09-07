# AirCoder — Product Instruction / Living Document

Development target: 0.1.0-beta.2
Updated: 2026-09-06

## Purpose

AirCoder chooses the economically appropriate executor for coding work and hands the task to an already existing execution path. It does not execute a second orchestration stack of its own.

## Canonical boundaries

- product source: `air-coder/` inside the `air-plugins` repository;
- entry skill: `skills/route-coding-task/SKILL.md`;
- deterministic selector: `skills/route-coding-task/scripts/select_executor.py`;
- result contract: `contracts/run-result.schema.json`;
- runtime/state: none owned by AirCoder;
- executor-specific state remains owned by RDC, Ruflo, Codex, Claude Code, or their existing components.

## Executor routes

- `chatgpt_rdc`: strong reasoning, low scarce-quota burden in the LPR working contour, slower repository hands;
- `ruflo`: substantial implementation where orchestration/parallelism justifies overhead;
- `native_cli`: fast Codex/Claude repository hands when speed benefit justifies scarce quota.

## Runtime truth rule

Selection is not proof of availability. The chosen route must be live-probed at execution time. On 2026-09-06 SRVLM01 exposed `codex-cli 0.145.0` and `Claude Code 2.1.226`; Ruflo was not a direct PATH command, while `npx @claude-flow/cli@latest --version` returned `ruflo v3.38.21`. These are observations, not pinned product configuration.

## Non-goals

AirCoder does not own a scheduler, watchdog, autonomous queue, controller, learning database, model router, billing layer, or provider transport. It does not absorb `air-worker` or `air-ruflo-bridge`.

## Release rule

The first beta is accepted only when selector tests pass, manifest/product versions match, all contract paths exist, three representative route decisions are reproduced mechanically, and the marketplace points to `./air-coder`. There is no previous AirCoder Stable; rollback is removal/disablement of the new plugin while the pre-existing executor paths remain unchanged.

## Known degraded mode

The root `air-plugins/.claude-plugin/marketplace.json` already fails `claude plugin validate` on historical `plugins[2]._moved` (missing `name/source`). The same two errors reproduce from pre-AirCoder HEAD. AirCoder's own plugin manifest validates successfully; the shared marketplace defect is not changed by this release task.
