# AirWorker 0.11.2 weekly auto-maintenance acceptance — 2026-09-28

Commit: `caeb548 air-worker: schedule weekly learning from curator tick`.

Contract:
- no separate scheduler/timer;
- `curator tick` runs weekly learning maintenance immediately when no prior weekly run exists;
- next run is due only after 7 days from deterministic `StartedAt`;
- weekly run keeps snapshot-before-mutation, 14d stale / 30d archive, REPORT.md + run.json and proposal-only merge review;
- weekly state is emitted in `air-worker.curator.tick/v3`.

Acceptance:
- targeted scheduling/course tests PASS;
- full Go regression PASS: 100.104s;
- `go vet ./...` PASS.
