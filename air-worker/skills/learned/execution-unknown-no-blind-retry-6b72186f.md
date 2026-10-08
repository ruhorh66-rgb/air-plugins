# execution-unknown-no-blind-retry

## When to apply
AIR Commander start_process returns EXECUTION_UNKNOWN or transport outcome is ambiguous.

## Procedure
1. Treat EXECUTION_UNKNOWN as unknown execution state, not failure. Do not repeat start_process until node/process/filesystem evidence proves the original command did not run.

## Pitfalls
- Verification: start_process returns EXECUTION_UNKNOWN -> inspect node/process/filesystem evidence and suppress duplicate start unless non-execution is proven
- Legacy proposal: LP-20260929T154021Z-c883876c
- Legacy approval: grant:LG-20260929T154302Z-e7900c1a
- Legacy verified at: 2026-09-29T15:43:24.6777016Z
- Legacy verification receipt: builtin:operational-procedure-v1:v1 structural=pass
