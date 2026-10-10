---
id: "N-112_p0-native-parent-validate-and-atomically-repair-p"
title: "P0-NATIVE-PARENT: validate and atomically repair plan node lineage with machine receipt"
parent: "N-110_p0-auto-capture-persist-every-real-airworker-ses"
trigger: "10.10.2026 LPR-directed plan optimization using native AirWorker: installed 0.11.8 plan node new accepted parent N-110_p0-auto-capture-persist-every-real-airworker-sess while actual N110 id ends with ...-ses. plan validate/lint incorrectly PASS despite dangling parent and return_to in N111. This is a real self-development defect and must be learned through safe native feedback, not direct plan file edits."
owner: "gpt-airworker-self-learning-20261010"
done_when: "Reject plan node new with non-existing parent before any durable PLAN/LEARN mutation; allow recognized PLAN.md root and exact existing N-* IDs only. Add native plan node reparent action with explicit node, expected old parent, exact existing new parent, actor/request-id and immutable receipt; under one kernel plan lock update only named node and PLAN spine atomically with rollback/error handling, correlated LEARN event, readback, crash/retry idempotence; reject missing parent, self/cycle/ambiguous IDs, closed/unowned foreign plan and contradictory request ID. Restore N111 to true N110 ...-ses through native kernel only and verify node/spine/receipt SHA match; no manual edit to plan/N-* or LEARN. Regression and vet, validate/lint, full exact Go, independent judge PASS and Git commit/push; no installed release change."
status: "open"
return_to: "N-110_p0-auto-capture-persist-every-real-airworker-ses"
created_at: "2026-10-10T13:05:25.8608136Z"
updated_at: "2026-10-10T13:05:25.8608136Z"
receipts:
---

# P0-NATIVE-PARENT: validate and atomically repair plan node lineage with machine receipt

- Родитель нити: N-110_p0-auto-capture-persist-every-real-airworker-ses
- Владелец: gpt-airworker-self-learning-20261010
- Готово когда: Reject plan node new with non-existing parent before any durable PLAN/LEARN mutation; allow recognized PLAN.md root and exact existing N-* IDs only. Add native plan node reparent action with explicit node, expected old parent, exact existing new parent, actor/request-id and immutable receipt; under one kernel plan lock update only named node and PLAN spine atomically with rollback/error handling, correlated LEARN event, readback, crash/retry idempotence; reject missing parent, self/cycle/ambiguous IDs, closed/unowned foreign plan and contradictory request ID. Restore N111 to true N110 ...-ses through native kernel only and verify node/spine/receipt SHA match; no manual edit to plan/N-* or LEARN. Regression and vet, validate/lint, full exact Go, independent judge PASS and Git commit/push; no installed release change.
