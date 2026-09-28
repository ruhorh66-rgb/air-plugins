# AirWorker 0.11.2 canonical learn journal acceptance — 2026-09-28

AirWorker commit `2cf1b93` makes legacy `learn/journal.jsonl` migration-only in core policy and permits only the explicit `air-worker learn migrate-legacy` path.

Real AirCurator acceptance:
- canonical AirCurator main before final delta: `37dff73`;
- live legacy journal imported through current AirWorker dev binary: first run `IMPORTED 1`, second run `IMPORTED 0`;
- canonical `learn/events.jsonl` changed by exactly one event (`api-state-stale`);
- only that canonical file was committed; concurrent dirty PLAN/journal/N-004/N-012/N-015 work was preserved untouched;
- AirCurator main commit/push: `cecb149 learn: import latest legacy journal event`.

Result: canonical events are losslessly current and repeated migration is idempotent.
