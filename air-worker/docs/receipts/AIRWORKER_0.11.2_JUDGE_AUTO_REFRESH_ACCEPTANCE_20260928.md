# AirWorker 0.11.2 factual judge auto-refresh acceptance — 2026-09-28

Commit: `bd13e06 air-worker: refresh factual judge on commit and tick`.

Contract:
- missing cached verdict => core reruns factual judge and republishes;
- verdict older than 6h, changed git HEAD, or changed judge fingerprint => rerun;
- fresh verdict => reused without rerun;
- successful `git commit` observed through shared PostToolUse triggers refresh; lifecycle failure remains fail-open with trace/context;
- `curator tick` carries `air-worker.judge.refresh/v1`; unmeasurable product is explicit code=2 and tick returns reject code 3.

Acceptance:
- targeted refresh/HEAD/PostToolUse/tick tests PASS;
- full Go regression PASS: 85.417s;
- `go vet ./...` PASS.
