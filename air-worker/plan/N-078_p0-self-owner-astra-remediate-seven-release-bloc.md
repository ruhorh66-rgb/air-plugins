---
id: "N-078_p0-self-owner-astra-remediate-seven-release-bloc"
title: "P0-SELF-OWNER-ASTRA: remediate seven release blockers from 6d9f973 review"
parent: "N-076_p0-self-owner-persistent-airworker-selector-dual"
trigger: "Independent gpt-6-astra high read-only review of exact clean 6d9f973 returned CHANGES_REQUIRED with seven P1 release blockers after targeted/full tests passed."
owner: "gpt-airworker-release-20261008"
done_when: "Remediate all seven Astra findings without weakening shared-learning invariants: (1) force LF checkout bytes for packaged skills/learned procedures and test core.autocrlf=true fresh checkout/bootstrap; (2) preserve immutable seed provenance while allowing supported managed procedure updates/rollback and validate current state through subsequent ledger history rather than requiring original migration SHA forever; (3) correlate EXECUTION_UNKNOWN trigger with diagnostics to the same operation/node/request/artifact and invalidate usage after any intervening retry; (4) supported MCP response envelopes treat isError/is_error/error and failed content as failure, never nonempty arrays as success; (5) self selector requires explicit boolean enabled and rejects duplicate JSON keys including enabled; (6) context receipt represents the exact current delivery snapshot, binds principal/session/run and never selects stale SHA from accumulated historical events; (7) Stop persists self and target run_completed before slow review and moves review to durable bounded/background mechanism compatible with hook timeout, so one owner cannot starve the other. Add negative regressions for every finding, full go test ./... + go vet, clean-session/fresh-checkout E2E, and independent gpt-6-astra high PASS on a clean immutable commit. Receipt must reference R:/-4-/air-worker/work/0.11.9/astra-6d9f973/verdict.json. No release/install/reboot/UAC/foreign-service action."
status: "closed"
return_to: "N-076_p0-self-owner-persistent-airworker-selector-dual"
created_at: "2026-10-08T09:04:20.3233261Z"
updated_at: "2026-10-08T15:02:18.6577087Z"
receipts:
  - "R:\\-4-\\air-worker\\work\\0.11.9\\n090\\N078_ACCEPTANCE_bc33045.json"
---

# P0-SELF-OWNER-ASTRA: remediate seven release blockers from 6d9f973 review

- Родитель нити: N-076_p0-self-owner-persistent-airworker-selector-dual
- Владелец: gpt-airworker-release-20261008
- Готово когда: Remediate all seven Astra findings without weakening shared-learning invariants: (1) force LF checkout bytes for packaged skills/learned procedures and test core.autocrlf=true fresh checkout/bootstrap; (2) preserve immutable seed provenance while allowing supported managed procedure updates/rollback and validate current state through subsequent ledger history rather than requiring original migration SHA forever; (3) correlate EXECUTION_UNKNOWN trigger with diagnostics to the same operation/node/request/artifact and invalidate usage after any intervening retry; (4) supported MCP response envelopes treat isError/is_error/error and failed content as failure, never nonempty arrays as success; (5) self selector requires explicit boolean enabled and rejects duplicate JSON keys including enabled; (6) context receipt represents the exact current delivery snapshot, binds principal/session/run and never selects stale SHA from accumulated historical events; (7) Stop persists self and target run_completed before slow review and moves review to durable bounded/background mechanism compatible with hook timeout, so one owner cannot starve the other. Add negative regressions for every finding, full go test ./... + go vet, clean-session/fresh-checkout E2E, and independent gpt-6-astra high PASS on a clean immutable commit. Receipt must reference R:/-4-/air-worker/work/0.11.9/astra-6d9f973/verdict.json. No release/install/reboot/UAC/foreign-service action.
