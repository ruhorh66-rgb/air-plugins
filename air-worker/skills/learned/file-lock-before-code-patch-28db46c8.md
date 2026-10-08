# file-lock-before-code-patch

## When to apply
Atomic rename/replace/write of an existing file returns ACCESS_DENIED or sharing violation.

## Procedure
1. On Windows ACCESS_DENIED replacing a file, identify the file-handle owner with Restart Manager or equivalent read-only diagnostics before changing atomic-write code; if a supervised process owns the handle, release it only through its approved lifecycle controller and retry unchanged code.

## Pitfalls
- Verification: file replacement returns ACCESS_DENIED -> identify handle owner before any write-path code patch
- Legacy proposal: LP-20260929T154021Z-7b217ff8
- Legacy approval: grant:LG-20260929T154302Z-9c5c390f
- Legacy verified at: 2026-09-29T15:43:24.7616983Z
- Legacy verification receipt: builtin:operational-procedure-v1:v1 structural=pass
