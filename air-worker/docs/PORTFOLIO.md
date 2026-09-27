# AirWorker portfolio registry (`air-worker.products/v1`)

`air-worker report -all`, `air-worker drift -all`, and `air-worker curator digest -all` read one explicit registry. The registry is configuration, not an inferred filesystem crawl: every product declares where its work lives and where its control plane is measured.

## Minimal schema

```json
{
  "schema": "air-worker.products/v1",
  "products": [
    {
      "id": "air-storage",
      "name": "AirStorage",
      "root": "F:\\-5-\\011_Plugins\\AirStorage_Wiki",
      "plan": "PLAN.md"
    }
  ]
}
```

`id` is unique and stable. `name` is display text. `root` is the runtime/work root. `control_root` is optional and defaults to `root`; it is the root containing `run-config.json`, the canonical PLAN, machine verdict and portfolio learning state. This split is required for products such as AirSync where source/runtime and Wiki control plane intentionally live in different trees.

`plan` resolves under `control_root`. `config` may override the default `control_root/run-config.json`.

## Plan adapters

The default adapter is `air-worker`: the PLAN uses AirWorker's numbered four-column/check-box grammar.

Legacy plans can declare a deterministic Markdown status-table adapter instead of being parsed heuristically:

```json
{
  "plan_adapter": {
    "kind": "status-table",
    "section_contains": "Development backlog",
    "id_prefix": "DEV-AL-",
    "title_column": 2,
    "status_column": 3,
    "done_statuses": ["done", "closed", "completed"],
    "gate_statuses": ["waiting_human"]
  }
}
```

Columns are zero-based. `section_contains` optionally scopes parsing to the first Markdown heading containing that text and stops at the next heading of the same or higher level; this is how one multi-release Wiki PLAN can expose only the current release table. Only rows matching `id_prefix` enter the measurement. The adapter reports open/closed/gate counts, next work and milestone distance. It is **not** a readiness judge.

## Distance sources

Each row exposes `effective_distance` and a mandatory `distance_source` whenever a numeric distance is available. Sources are chosen in this order:

1. **Native AirWorker judge** — when `run-config.json` exists and names the same PLAN. Source: `judge:air-worker/v1`.
2. **External JSON metric** — when the registry explicitly names the JSON path, numeric field, target and direction.
3. **Plan milestone fallback** — executable rows before the next gate/end. Source begins `milestone:`.

A fallback is intentionally labelled as a fallback; it never pretends to be a goal verdict.

An external numeric metric is declarative and uses no shell or LLM:

```json
{
  "metric": {
    "kind": "json-number",
    "path": "receipts/goal-metrics-last.json",
    "field": "overall_score",
    "target": 100,
    "direction": "higher_better",
    "scale": 100
  }
}
```

`higher_better` computes the positive shortfall to target; `lower_better` computes the positive excess above target. `scale` converts the floating-point shortfall to an integer distance before ceiling. A missing file/field remains unavailable; text in a Markdown plan is never substituted for a machine metric.

## Judge refresh

A registry row may opt in to:

```json
{ "refresh_judge": true }
```

This makes `report -all` / `drift -all` run the product's declared AirWorker judge and publish a fresh machine verdict before reading distance. The default is false because judges may be expensive. Refresh is refused while the product loop lock is held, so a portfolio report cannot race the loop's own verdict.

## Portfolio drift

`air-worker drift -all -registry <path> [-record] [-history <path>] [-json]` compares portfolio snapshots without inventing another goal rule.

For the same product, `distance_source`, and plan contract:

- distance down **or** closed-step count up = `FORWARD`;
- distance up **or** closed-step count down = `REGRESS`;
- unchanged comparable signals = `STALL`;
- a source/plan-contract change starts `BASELINE_SOURCE_CHANGE`;
- no measurable distance and no plan signal = `UNKNOWN`, never zero.

The closed-step signal is essential for products such as AirSync whose judge distance can remain zero while additional plan steps are still being completed.

History defaults to `.air-worker/portfolio-drift.jsonl` next to the registry and is separated by product and source.

## Lookup order

`-registry <path>` wins. Without it, AirWorker checks `AIR_WORKER_PRODUCTS_FILE`, then the installed AirWorker state directory `air-worker-products.json`, then `.air-worker/products.json` in the current directory.

The registry is machine/installation configuration and should not embed one server's paths into the portable plugin manifest.

## 10:00 digest

`air-worker curator digest -all -registry <path>` reuses the same effective portfolio measurement and adds:

- `Ждёт да`: all `PENDING_LPR` learning proposals found under registered product roots;
- `Самообучение`: measured repeat counts after applied rules.

No LLM recomputes these statistics.
