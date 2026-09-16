# AirWorker 0.10.6 release acceptance — 2026-09-17

Scope: closed steps 85, 85а, 86 only. Multi-session kernel remains deferred to 0.10.7.

## Factual acceptance

- Targeted K67-K71: PASS.
- Step 85: K67/K68/K69 factual PASS; Codex semantic PASS, step_done=yes, drift=none.
- Step 85а: K71 factual PASS; Codex semantic PASS, step_done=yes, drift=none.
- Step 86: K70 factual PASS; Codex semantic PASS, step_done=yes, drift=none.
- Full `go test ./...`: PASS.
- Full `go vet ./...`: PASS.
- `git diff --check`: PASS.
- plugin portability/version check: PASS.
- `air-worker goals`: 7 goals / 68 criteria / valid=yes.

## Live release acceptance

- Temporary canonical-plan copy: goals valid=yes before feedback and valid=yes after feedback.
- `air-worker feedback`: exit 0, status OK, immutable evidence + linked non-executable PLAN candidate.
- `tool -which router`: `runner=router`, `shell=opencode`, `AirLLMRouter`, exit 0.
- default `tool -product ...`: Router visible because active Haiku runner is `kind=router`.
- `tool -which opencode`: installed OpenCode resolved successfully.
- AirLLMRouter version probe: 0.3.0.
- OpenCode version probe: 1.18.31.
- Live Start Menu shortcut exists; invoking it while tray is already running preserved one tray process (1 -> 1).

## Release profile

`script -> haiku:medium -> haiku:max -> sonnet:medium -> sonnet:max -> opus:medium -> opus:max`.
Haiku uses AirLLMRouter. Sonnet/Opus remain direct. Codex remains independent semantic reviewer.
