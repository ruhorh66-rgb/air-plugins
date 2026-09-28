# AirWorker 0.11.2 GPT lifecycle source acceptance — 2026-09-28

Scope: N-003 source/core parity only. N-004 remains open until AIR Commander middleware is released/deployed through the standard path.

AirWorker:
- `020667b` trusted signed ChatGPT lifecycle host provenance.
- `1219bcd` GPT Stop requires non-empty evidence transcript.
- host-neutral session identity + JSON status; shared SessionStart/UserPromptSubmit/PreToolUse/PostToolUse/Stop handlers.

AIR Commander source:
- `a9fb77d` signed ChatGPT session attach, automatic PreToolUse/PostToolUse, Stop finalizer, approval UI/token path.
- `cbc14b9` minimal runtime transcript for GPT lifecycle; tool arguments are not persisted.

Acceptance evidence:
- AIR Commander full regression: 95 tests total, 94 PASS, 0 FAIL, 1 Windows symlink fixture SKIP.
- current isolated bridge + real dev AirWorker binary: attach=PASS, PreToolUse=PASS, PostToolUse=PASS, Stop=PASS, transcript lines=4, tool arguments absent.
- disposable approval E2E: pending proposal -> UI-scoped token -> trusted ChatGPT UserPromptSubmit -> one-use grant -> learn apply -> executable verification cases=4/4 -> pending queue empty.
- fail-open/fail-closed semantics covered by Go and bridge tests; direct untrusted GPT hook calls cannot mint grants.

Not claimed here:
- live production AIR Commander hook transport (N-004).
- two-node released 0.11.2 acceptance (N-002).
