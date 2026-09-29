# AirWorker 0.11.3 N-016 Headroom acceptance — 2026-09-29

Node: SRVLM01
Plan node: N-016_0-11-3-anthropic-judges-through-headroom
Source commit: 6fc04a7

## Accepted facts

- canonical Anthropic base URL is http://localhost:8787.
- AirWorker forces ANTHROPIC_BASE_URL through shared runnerEnv/applyClaudeRunnerEnv for Claude executor, orchestrated executor, planner and semantic judge launch paths.
- an inherited direct ANTHROPIC_BASE_URL override is replaced by the Headroom URL.
- existing user-level CLAUDE_CODE_OAUTH_TOKEN behavior is preserved and the value is never logged.
- air-worker tool -which headroom performs a read-only /health preflight.

## Verification

- Headroom health PASS: ready=true, version=0.39.0.
- targeted Go tests PASS, including forced Headroom env and direct-base-URL bypass rejection.
- go vet ./... PASS.
- git diff --check PASS.
- real normal-mode Claude smoke via the canonical AirCurator run-subagent Headroom path:
  - model claude-sonnet-5-5
  - effort low
  - terminal=completed
  - is_error=false
  - canonical model=claude-sonnet-5-5
  - result=HEADROOM_OK
- smoke temporary task/output files removed.

Ponytail/Codex transport remains N-017.
