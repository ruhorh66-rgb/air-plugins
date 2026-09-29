# AirWorker 0.11.3 N-017 Ponytail acceptance — 2026-09-29

Node: SRVLM01
Plan node: N-017_0-11-3-openai-executors-through-ponytail
Source commits: 5894371, 7081b14

## Standard vendor install

Ponytail was installed through the official Codex plugin CLI, not by copying files:

- codex plugin marketplace add DietrichGebert/ponytail — RC=0
- codex plugin add ponytail@ponytail — RC=0
- enabled plugin: ponytail@ponytail
- marketplace source: https://github.com/DietrichGebert/ponytail.git
- installed plugin version: 4.10.0
- installed skill: CODEX_HOME/plugins/cache/ponytail/ponytail/4.10.0/skills/ponytail/SKILL.md

## Non-interactive finding and fix

A strict fresh-thread smoke proved that non-interactive codex exec did not expand @ponytail by itself, even after official plugin install: it returned NOT_LOADED. AirWorker therefore does not equate an @ponytail marker with skill activation.

AirWorker now:
- requires enabled ponytail@ponytail from the approved vendor source;
- resolves the newest installed vendor SKILL.md from the standard Codex plugin cache;
- fails closed before starting Codex when the plugin/skill cannot be proven;
- supplies the exact installed vendor skill body with ACTIVE MODE: full for executor, orchestration subagents/leader, planner, residual Codex semantic/plan review and LEARN review;
- keeps @ponytail full as provenance, but does not depend on interactive session expansion;
- exposes air-worker tool -which ponytail preflight.

## Verification

- targeted Ponytail/Codex tests PASS.
- tests cover vendor source, newest semantic version, executor/planner/semantic context, and fail-closed before process start.
- go vet ./... PASS.
- git diff --check PASS.
- candidate preflight: ponytail plugin ready, version 4.10.0.
- real Codex smoke:
  - model gpt-6-luna
  - effort low
  - sandbox read-only
  - prompt contained the actual installed vendor SKILL.md
  - no tools were needed by the model
  - turn.completed
  - response reproduced vendor ladder rung 7: "Only then: the minimum code that works"
- smoke temporary files removed.

No AirWorker token telemetry was added.
