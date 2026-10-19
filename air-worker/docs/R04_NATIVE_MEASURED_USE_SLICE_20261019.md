# AirWorker 0.11.9 · R04 native measured learned-procedure use

Scope: source-only Worker B, published https://github.com/ruhorh66-rgb/air-modules/tree/v0.1.0. No new module API, reviewer loop, tag, release or install. R04 remains PLANNED in the canonical plan until the sequential release-plan writer accepts R02/R03.

## State and real operation

R02 signs the original CLI preflight; R03 records the production reviewer and safe APPLIED procedure. R03 CLOSED is historical and cannot itself authorize R04.

R04 requires R02 CLOSED / R03 CLOSED / R04 OPEN / R05 PLANNED, candidate binary SHA, signed original failure, current reviewer/APPLIED/target@SHA/index, and exact immutable R03 receipt. The intermediate release-plan states are reconstructed byte-for-byte.

R02 now includes disposable target_root in its HMAC-signed outer release envelope. R04 permits only an immediate child of the configured scratch root with the prescribed random prefix, validates the original CLI invocation HMAC and recreates only that empty directory when missing. Raw original arguments remain absent from LEARN; user files are never deleted or overwritten.

R04 first publishes exactly one immutable **HMAC release-open authorization**, bound to the active R04 OPEN plan SHA, original signed source run/event, reviewed target@SHA, candidate binary and R03 receipt. Its timestamp MUST precede the native N093 signed intent. This prevents retroactively crediting real use between R03 reviewer publication and R04 OPEN. A prior authorization with no terminal proof refuses blind retry.

The scratch directory is opened by Go os.Root as a physically pinned directory handle; every ancestor must be an ordinary non-reparse directory. The random original child is created through the pinned root, never an unverified textual path, and its opened handle's final path must match the signed original scratch target.

R04's next actual operation is AirWorker validate in a NEW process on that same signed scratch target. Published learning v0.1.0 and compiled N093 safe read-only action emit the only accepted procedure_used. An existing signed result is recovered without invoking the operation again; a pending intent without a final result stops blind retries.

## Effect and release transition

R04 independently checks current module reviewer/APPLIED/index/target SHA, signed original CLI failure, reviewer HMAC records, exact skill_loaded, one module-owned procedure_used, and separate HMAC-signed intent and terminal result. Only the effect avoided_repeat_validation_with_missing_config and deferred_missing_configuration may pass; native validation did NOT run.

After exact readback, R04 publishes one immutable receipt binding R02/R03 SHA, candidate binary, current release-plan, module event, intent/result and bounded effect. Native AirWorker release lock and exact-byte compare-and-swap change only R04 OPEN to CLOSED and R05 PLANNED to OPEN. Rolled-back current learned procedure fails closed; later module rollback requires R05/R06 to reverify their own gates.

## Source-only QA and rollback

Positive fixture: one real signed learned use after R03; one immutable R04 receipt; R05 OPEN; replay cannot create a second event or reviewer. Negative fixtures: loaded is not used; untrusted positive JSON lacks signed intent/module event; rollback denies; stale plan and duplicate stage reject. Full source Go test/vet/build plus one Sol medium, then ONE new disposable production R02-R03-R04 on identical frozen source and binary SHA.

Installed 0.11.8, published air-modules v0.1.0, canonical PLAN/N/LEARN and foreign worktrees remain unchanged. Release/tag/install require separate explicit LPR approval.
