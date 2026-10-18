# N-124: immutable GitHub branch audit (source-only)

State: **ADVISORY / NO AUTHORITY / NO PLAN WRITES**. This command is implemented in the AirWorker development source branch, not the installed 0.11.8 CLI. This is one factual substep toward the N-124 canonical Git-generation contract; it is **not** the trusted publisher.

## Invocation

Build the isolated source candidate in a separate output directory. Do not install it or modify the globally installed AirWorker. From a verified product source root:

```powershell
$exe = 'R:\-4-\air-worker\work\N124_WORKER_A_20261018\air-worker-n124-authority-audit-sourceonly.exe'
& $exe plan authority audit -product 'R:\-4-\air-worker\worktrees\airworker-n124-worker-a-20261018\air-worker' -canonical-branch 'dev/airworker-n124-worker-a-20261018' -canonical-head 'e1e113a402393a3048c500b32dff3b81e7582a8e' -peer-branch 'dev/airworker-n124-gpt-0119-20261018' -peer-head '7feeaa16848ede938e5d1291f1d8b793679fd125' -json
```

The pinned GitHub remote is `https://github.com/ruhorh66-rgb/air-plugins.git`. This audit requires both exact branch SHA readbacks, local immutable commit objects, a clean product PLAN/node projection and no unsafe local Git URL/credential/proxy rewrite. GitHub HEAD is reread at the end, because a stable first read proves no final generation.

The audit compares the numeric N-ID, full ID and SHA-256 of each native node blob from the two Git commits; their `PLAN.md` must contain the native marker and unique links to those node IDs. A reused number with different ID is `numeric_id_reallocated`; identical ID with different bytes is `same_id_content_diverged`. Missing Git object, invalid node, unexpected Git remote, an invalid ref or a remote race is `NOT_PROVEN`. A real collision is `CONFLICT`.

Even with no collision, the only affirmative text is `NO_COLLISIONS_LOCAL_ONLY`; `authority_granted=false`, `changed=false`, `exit_code=2` always. **Never use this report as proof of permission to mutate canonical PLAN, plan/N-*, LEARN, or GitHub refs.** It is deliberately not wired into native `plan node new/close` as a trust or privilege input.

## Completion criteria / next steps

- This slice: Go unit+adversarial and Git-fixture tests PASS; actual GitHub refs readback as pinned; source `go vet` + build RC0, source SHA committed and remotely verified; independent Codex Sol low P0/P1=0 on exact SHA; source-bound machine receipt.
- N-124 remains OPEN until one independently trusted GitHub publisher performs atomic generation-specific CAS, proves N120/121 uniqueness with stale two-client races and crash recovery, and verifies installed binary/product authority, without relaxing fixture guards.
- DEV-136 and AirLegal N-111 must retain BLOCKED until native N-124 authority plus installed-binary attestation are accepted.
- Rollback this audit by reverting its own source-only commit. Installed AirWorker 0.11.8 and all foreign WIP remain untouched.
