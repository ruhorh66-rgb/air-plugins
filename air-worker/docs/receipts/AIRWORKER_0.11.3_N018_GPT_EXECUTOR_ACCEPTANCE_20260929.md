# AirWorker 0.11.3 N-018 GPT executor core acceptance — 2026-09-29

Node: SRVLM01
Plan node: N-018_0-11-3-gpt-executor-core-commands
Source commits: cdef68a, 8fe2886, 4ec80c7

## Core surface

- `air-worker executor list -product <root> [-json]`
- `air-worker executor dispatch -product <root> -class bulk|standard|complex|hardest -task-file <path> -principal <p> -session-key <k> [-tier <approved-tier>] [-json]`

Dispatch reuses the existing Codex runner, Ponytail preflight/context, model policy, session scope and durable job receipts. It does not add a second orchestration engine or token telemetry.

## Policy readback

Candidate `executor list -json` returned:
- primary: GPT-6 Luna low/medium/high; GPT-6 Sol low/medium/high; GPT-6 Astra low/medium/high;
- registered GPT-5.5/5.6 fallback variants only at low/medium/high;
- classes start at low and cap at high.

## Session and smoke

- isolated session `chatgpt__n018-smoke` was active and declared for the exact AirWorker product root.
- Ponytail preflight: installed vendor plugin version 4.10.0.
- real dispatch: `gpt6-luna:low` -> `gpt-6-luna`, effort low.
- receipt: principal `chatgpt`, session `n018-smoke`, provider `openai`, role `executor/leader`, sandbox `workspace-write`, process_started=true, status DONE.
- task execution fact: disposable `.woody/n018-smoke-src/calc.go` changed exactly from `return 0` to `return a + b`.
- result contained `GPT_EXECUTOR_OK`.
- targeted executor/Ponytail/Codex tests, go vet and diff check PASS.
