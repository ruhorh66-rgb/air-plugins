# AirWorker 0.11.3 N-021 Codex stdin transport acceptance — 2026-09-29

Node: SRVLM01
Plan node: N-021_0-11-3-codex-multiline-prompt-transport-via-stdi
Source commit: 4ec80c7

## PASS facts

- All AirWorker Codex executor, orchestration subagent/leader, planner and LEARN review paths use the Codex stdin contract: `codex exec ... -`.
- Model, effort, sandbox and product root remain argv; the multiline Ponytail vendor skill + AirWorker task payload is written to stdin.
- Targeted `Test(Codex|Ponytail|Executor|Planner)` suite PASS.
- `go vet ./...` PASS.
- `git diff --check` PASS.
- Live process inspection showed the N-018 Codex child command ending in `model_reasoning_effort=low -`, with no multiline task argv.
- Real GPT executor smoke:
  - principal/session: `chatgpt / n018-smoke`
  - class/tier/model/effort: `bulk / gpt6-luna:low / gpt-6-luna / low`
  - durable receipt status: DONE
  - result included `GPT_EXECUTOR_OK`
  - disposable target changed from `return 0` to `return a + b`.
