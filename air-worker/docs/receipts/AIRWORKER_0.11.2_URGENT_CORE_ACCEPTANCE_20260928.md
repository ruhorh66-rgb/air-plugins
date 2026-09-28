# AirWorker 0.11.2 urgent core acceptance — 2026-09-28

Branch: `dev/airworker-0.11.2-gpt-learn-lifecycle`

Accepted source commits:
- `923702a` — GPT lifecycle identity, verdict freshness, daily partner.
- `9f28095` — rule usage telemetry and weekly lifecycle.
- `020667b` — trusted signed ChatGPT lifecycle host path.
- `2cf1b93` — generic executable learned-check registry and canonical legacy journal enforcement/import.
- `4e25adb` — curator course-control in tick v3.

Acceptance:
- targeted learn/course suite: PASS (`go test . -run ...`, 2.292s).
- full Go regression: PASS (`go test . -count=1`, 100.025s).
- `go vet ./...`: PASS.
- generic executable registry proves hook/gate/script transactions and fail-closed missing implementation.
- curator tick v3 emits course progress, >1h stalls, idle windows, pending gates and deterministic actions.
- isolated real-binary ChatGPT lifecycle smoke: attach → SessionStart → PreToolUse → PostToolUse → Stop PASS.
- disposable end-to-end approval: pending proposal → UI-scoped token → trusted ChatGPT UserPromptSubmit → one-use grant → `learn apply` → executable verification `cases=4/4` → pending queue empty.

Not closed by this receipt:
- N-006 real AirCurator post-migration rows still require canonical import.
- N-004 requires standard deployment of AIR Commander middleware before live hook parity can be claimed.
- N-002 requires two-node live acceptance after the 0.11.2 release/install.
