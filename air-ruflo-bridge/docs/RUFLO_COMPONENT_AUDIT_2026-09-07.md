# Ruflo component audit — 2026-09-07

Status: current upstream revalidation for AIR OS / AirCoder Ruflo route.

## Why this exists

The previous AIR internal note from 2026-08-14 evaluated only a small subset of Ruflo plugins and described `ruflo-core` mainly as an MCP donor. That description is now stale. Current upstream Ruflo exposes a much broader plugin catalog and explicitly separates plugin-lite installation from the full CLI loop.

## Current upstream facts

- Upstream repository: `ruvnet/ruflo`.
- Current verified release for this AIR runtime: `v3.38.21`.
- Upstream README currently lists about 35 native plugins.
- Upstream production/full-loop path is CLI init/runtime: agents + commands + skills + MCP + hooks + daemon.
- Claude plugin installs are a lighter surface and are not equivalent to the full loop.
- `ruflo-core` is now a foundation surface with server/health/plugin discovery and a broad MCP/tool catalog; the old donor-only assumption is superseded.
## AIR adoption policy

The AIR Ruflo route does **not** install all upstream plugins blindly. Correctness is based on full-loop capabilities, not on plugin count.

Required for the AirCoder Ruflo route:
- Ruflo CLI engine and pinned rollback;
- MCP registration synchronized to the selected engine;
- hooks/daemon/swarm/autopilot surfaces used by the canonical bridge;
- working Claude launch path on Windows;
- Agent Booster/`agentic-flow` link required by the existing bridge;
- objective-file handoff and dry-run/approval sequence through `run_task.ps1`.

Recommended/candidate add-ons:
- `ruflo-cost-tracker` — executor economics and spend receipts;
- `ruflo-observability` — structured run telemetry;
- `ruflo-goals` — large-goal decomposition;
- `ruflo-aidefence` — prompt-injection/PII hardening;
- `ruflo-metaharness` — optional harness audit only, never a runtime dependency.
Do not adopt by default:
- `ruflo-rag-memory` / memory-knowledge plugins when they would duplicate AIR Storage ownership;
- `ruflo-loop-workers` when AIR automation controllers already own scheduling;
- `ruflo-federation` for single-host SRVLM01 work;
- `ruflo-ruvllm` while AIR LLM Router owns model routing and local LLM policy;
- domain-specific plugins unless a task explicitly needs them.

## Engine update performed

AIR runtime engine was updated additively from `3.38.11` to `3.38.21` on 2026-09-07. Rollback remains installed and pinned as `3.38.11`, which has a successful AIR run from 2026-09-06.

The new npx cache was verified to contain `@claude-flow/cli 3.38.21`; MCP config was re-synchronized to that cache. The bridge-required `agentic-flow` junction was re-established after preserving the vendor copy. No Ruflo swarm was launched during this audit.

## Release rule

Every future Ruflo engine upgrade must first re-check upstream release notes **and** the component catalog. AirCoder's Ruflo preflight must pass before the canonical bridge is allowed to produce a dry-run.
