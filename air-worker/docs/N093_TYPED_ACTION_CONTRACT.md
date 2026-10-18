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

## Production reviewer-compatible typed action — N093 P1

The published air-modules v0.1.0 requires Markdown headings. A machine-only
envelope cannot be applied by that module as a new procedure, so the production
reviewer now offers the following strictly constrained Markdown capsule ONLY
for an observed native run_completed missing-configuration failure:

    # AirWorker Native Action

    ## When to apply

    After an authenticated native validate failure because canonical run-config.json is missing.

    ## Procedure

    ~~~air-worker-action
    {"schema":"air-worker.native-learned-action/v1","id":"validate.canonical-config-preflight","version":1,"native_command":"validate","precondition":"validate-run-config-missing","operation":"check-missing-config-and-defer","outcome":"deferred_missing_configuration","effect":"avoided_repeat_validation_with_missing_config"}
    ~~~

    ## Pitfalls

    - This read-only action neither repairs files nor claims that validation passed.

The wrapper is exact, source-owned and never free-form model authority.
JSON property whitespace may vary, yielding a new reviewed target@SHA,
but all eight fields must match the compiled registered read-only action.
Changed surrounding text, extra bytes, duplicate/unknown JSON keys, unsafe
commands and unsupported versions are rejected. Reviewer output alone has no
permissions: a signed source CLI failure, APPLIED ledger, unique target SHA,
load and native OS precondition remain mandatory before use. The old
machine-only envelope is retained for backward compatibility, but does not
pass the published module's procedure validator by itself.

For compatibility, the N118 audited safe procedure and two independently
reviewed N093 safe preflight examples from 18.10.2026 have **exact content**
source-owned grammars. The second production reviewer produced an independently
APPLIED procedure at SHA `266805f1...28552b8`; its action is still the same
compiled, read-only, non-validating preflight. Neither the source nor the
runtime grants an action merely because a new digest exists. Any other model
Markdown stays `UNSUPPORTED` until a separate compiled action/negative gate;
the Markdown-compatible typed capsule is the production extensibility path.

## Machine use provenance

The native operation requires: signed original CLI failure and invocation,
unique matching source event, reviewer candidate whose bytes equal the
current applied file, unique APPLIED ledger row and module index, exact
current target SHA, no linked file path, correct active product owner and
machine-verified precondition. Under a serialized lock it first persists an immutable **signed intent**
(status=pending_procedure_used; effect=NOT_PROVEN) with action ID/version,
source run/event, session/principal, process PID, deterministic nonce,
source-owned PLAN.md fingerprint (including absence) and product-private HMAC.
The native module must then persist exactly one matching procedure_used event
pointing at the intent SHA. A separate final signed terminal receipt is
created **only after** exact module event readback and recheck of the current
review/APPLIED ledger, rule SHA, missing-config OS condition and pinned PLAN
fingerprint. Its HMAC binds the immutable intent SHA and module event ID.
Replay cannot rewrite either receipt or create another event. A crash after
intent or event is resumable under the same idempotency lock. Signed-receipt
or journal tampering, drift of PLAN/config, wrong action or source identity
and a missing terminal event fail closed, without claiming validation PASS.

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
