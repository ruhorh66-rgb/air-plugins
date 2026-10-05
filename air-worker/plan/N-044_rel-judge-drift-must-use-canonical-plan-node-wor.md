---
id: "N-044_rel-judge-drift-must-use-canonical-plan-node-wor"
title: "REL-JUDGE: drift must use canonical plan-node work source"
parent: "N-041"
trigger: "Exact-commit judge 9c709ae4 PASSed 13/13 checks and 65/65 criteria with distance 0, but drift returned WAIT_LPR because it counted 18 legacy PLAN table rows while report next already uses plan-node spine."
owner: "gpt-airworker-learning-20261005"
done_when: "When canonical plan nodes exist, drift/report/PlanState use the same active work source: open node spine, not legacy table rows. Legacy rows remain history and are not manually struck. Regression proves a product with judge distance 0 + legacy open rows + open plan nodes does not trigger WORKER-DRIFT-04 solely from legacy rows; node work remains visible separately. Existing legacy-only products preserve old behavior. Full Go tests/vet, plan validate, factual judge and independent Astra PASS. Receipt docs/receipts/LEARNING_20261005_DRIFT_NODE_SOURCE.json."
status: "open"
return_to: "N-041"
created_at: "2026-10-05T16:41:04.2419599Z"
updated_at: "2026-10-05T16:41:04.2419599Z"
receipts:
---

# REL-JUDGE: drift must use canonical plan-node work source

- Родитель нити: N-041
- Владелец: gpt-airworker-learning-20261005
- Готово когда: When canonical plan nodes exist, drift/report/PlanState use the same active work source: open node spine, not legacy table rows. Legacy rows remain history and are not manually struck. Regression proves a product with judge distance 0 + legacy open rows + open plan nodes does not trigger WORKER-DRIFT-04 solely from legacy rows; node work remains visible separately. Existing legacy-only products preserve old behavior. Full Go tests/vet, plan validate, factual judge and independent Astra PASS. Receipt docs/receipts/LEARNING_20261005_DRIFT_NODE_SOURCE.json.
