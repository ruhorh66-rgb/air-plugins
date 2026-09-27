# AirWorker portfolio registry (`air-worker.products/v1`)

`air-worker report -all` and `air-worker curator digest -all` read one explicit registry. The registry is configuration, not an inferred filesystem crawl: every row names the product runtime root and the canonical PLAN that should be measured.

## Minimal schema

```json
{
  "schema": "air-worker.products/v1",
  "products": [
    {
      "id": "air-storage",
      "name": "AirStorage",
      "root": "F:\\-5-\\011_Plugins\\AirStorage_Wiki",
      "plan": "F:\\-5-\\011_Plugins\\AirStorage_Wiki\\PLAN.md"
    }
  ]
}
```

`id` is unique and stable. `name` is display text. `root` is the runtime/product root used for native goal-distance when that contract exists. `plan` may be inside the root or an external Wiki file. Relative paths resolve from the registry directory for `root` and from the product root for `plan`.

## Resolution and truthfulness

The portfolio report always tries to parse the named PLAN with the AirWorker machine-plan grammar. If it cannot, `plan_contract=NOT_PROVEN` and the row stays visible with a named limit. It does not guess from prose.

Distance to the nearest plan milestone is the number of open executable rows before the next open gate. Native goal-distance is shown only when the product has `run-config.json` and its configured PLAN is the same file named by the registry. An external Wiki plan can therefore provide milestone distance while native goal-distance stays unknown.

Unknown is never converted to zero. A missing judge verdict, missing run config, mismatched runtime plan, or non-machine-readable plan is reported in `limits[]`.

## Lookup order

`-registry <path>` wins. Without it, AirWorker checks `AIR_WORKER_PRODUCTS_FILE`, then the installed AirWorker state directory `air-worker-products.json`, then `.air-worker/products.json` in the current directory.

The file is intentionally machine/installation configuration and should not embed one server's paths into the portable plugin manifest.

## 10:00 digest

`air-worker curator digest -all -registry <path>` reuses the same portfolio measurement and adds two deterministic sections:

- `Ждёт да`: all `PENDING_LPR` learning proposals found under registered product roots;
- `Самообучение`: measured repeat counts after applied rules.

No LLM recomputes these statistics.
