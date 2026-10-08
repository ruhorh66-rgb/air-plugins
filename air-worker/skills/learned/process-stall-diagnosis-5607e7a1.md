# process-stall-diagnosis

## When to apply
A running tool/process has no new output, a polling call looks stalled, or the operator suspects the worker is hung.

## Procedure
1. When a long process appears silent, inspect the original PID existence, age, CPU, descendants and completed output before declaring a hang or retrying; never duplicate a heavy start solely because polling returned no new lines.

## Pitfalls
- Verification: no new output from an existing or recently completed PID -> inspect original process/completion evidence before any retry
- Legacy proposal: LP-20260929T154021Z-33fd0e0e
- Legacy approval: grant:LG-20260929T154302Z-dfbdae5a
- Legacy verified at: 2026-09-29T15:43:24.5882373Z
- Legacy verification receipt: builtin:operational-procedure-v1:v1 structural=pass
