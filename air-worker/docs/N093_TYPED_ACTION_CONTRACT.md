# AirWorker N-093 — source-only native typed-action contract

Status: candidate, not released. Owner: Worker B.
Baseline: f226e42f2819c59c5887ccec0f10c0796219de08; air-modules v0.1.0.

## Compiled permission boundary

Only the compiled Go core executes native actions. Learned Markdown never grants
shell, file write, approval or execution privileges. The only registered
machine action in this candidate has:

- schema: air-worker.native-learned-action/v1
- id: validate.canonical-config-preflight
- version: 1
- native_command: validate
- precondition: validate-run-config-missing
- operation: check-missing-config-and-defer
- outcome: deferred_missing_configuration
- effect: avoided_repeat_validation_with_missing_config

Missing canonical run-config.json is verified by a native read-only OS check.
The validator is NOT executed and the CLI returns code 2. The measured effect
is avoiding a redundant retry, NOT repairing configuration or validating the
product. New machine operations require new source-owned handlers and tests.

## Generated machine-only format

A future reviewer may generate a machine-only procedure:

    # AirWorker Native Action

    ~~~air-worker-action
    {"schema":"air-worker.native-learned-action/v1","id":"validate.canonical-config-preflight","version":1,"native_command":"validate","precondition":"validate-run-config-missing","operation":"check-missing-config-and-defer","outcome":"deferred_missing_configuration","effect":"avoided_repeat_validation_with_missing_config"}
    ~~~

This must be the entire body with no extra model-authored instructions.
Duplicate and unknown JSON fields, extra bytes, unsafe commands, and
unsupported versions are rejected. Whitespace differences are permitted
without any new SHA allowlist. Reviewer output by itself is not authorization.

For compatibility, the N118 audited safe procedure and exact N093 reviewed
safe example from 18.10.2026 have bounded source-owned grammars; arbitrary
additional Markdown text remains unsupported.

## Machine use provenance

The native operation requires: signed original CLI failure and invocation,
unique matching source event, reviewer candidate whose bytes equal the
current applied file, unique APPLIED ledger row and module index, exact
current target SHA, no linked file path, correct active product owner and
machine-verified precondition. Under a serialized lock it then persists
one immutable action receipt with action ID/version, source run/event,
session/principal, actual process PID, nonce and product-private HMAC.
Exactly one module-owned procedure_used event points at its SHA.
Replay does not modify the receipt or create another event. Signed
receipt tampering, wrong action, wrong SHA and missing preconditions fail closed.

## Acceptance limits

B0 checks source and immutable prior R02/R03/R04 receipts.
B1 is scoped source-only development and Go fixture TDD, independent
review and full QA are separate gates. A Go fresh-process fixture is
NOT proof of a production Codex reviewer. The original R02/R03 job was
one-shot; its scratch target was removed during original cleanup, so
repeating it by guessing argv is forbidden. B2 requires a fresh
authenticated production candidate run with observable use and effect.
B3 and B4, native N-093 closure, publish/install and any UAC, reboot
or foreign-service stop remain separately gated.
