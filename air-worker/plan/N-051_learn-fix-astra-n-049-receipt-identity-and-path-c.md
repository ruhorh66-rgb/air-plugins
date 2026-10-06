---
id: "N-051_learn-fix-astra-n-049-receipt-identity-and-path-c"
title: "LEARN-FIX: Astra N-049 receipt identity and path confinement"
parent: "N-049"
trigger: "Independent gpt-6-astra 600s review on exact 4466c9a654162905aa961574d8494d485e6e77ee returned CHANGES_REQUIRED with three P2 findings inside N-049 scope."
owner: "gpt-airworker-learning-20261005"
done_when: "Close all three residual N-049 findings without scope expansion: (1) observe event committed before deferred trace failure must not cause context abort with an unqualified skill_loaded receipt; committed receipt remains delivery-success or is durably compensated; (2) context receipt run_id includes a digest of kind plus exact loaded/failure payload so identical retries dedupe but changed bytes/failures record separately; (3) independent reread rejects linked/reparse ancestors and proves the opened skill remains under the product root. Add real filesystem regressions for post-event trace failure, repeated same-session changed procedure/failure, and parent-link swap after index. Targeted/full Go, exact clean provenance, factual judge and Astra 600s PASS. Receipt docs/receipts/LEARNING_20261006_N049_ASTRA2.json."
status: "closed"
return_to: "N-049"
created_at: "2026-10-06T03:01:21.1672556Z"
updated_at: "2026-10-06T04:40:28.3767274Z"
receipts:
  - "docs/receipts/LEARNING_20261006_N049_ASTRA2.json"
---

# LEARN-FIX: Astra N-049 receipt identity and path confinement

- Родитель нити: N-049
- Владелец: gpt-airworker-learning-20261005
- Готово когда: Close all three residual N-049 findings without scope expansion: (1) observe event committed before deferred trace failure must not cause context abort with an unqualified skill_loaded receipt; committed receipt remains delivery-success or is durably compensated; (2) context receipt run_id includes a digest of kind plus exact loaded/failure payload so identical retries dedupe but changed bytes/failures record separately; (3) independent reread rejects linked/reparse ancestors and proves the opened skill remains under the product root. Add real filesystem regressions for post-event trace failure, repeated same-session changed procedure/failure, and parent-link swap after index. Targeted/full Go, exact clean provenance, factual judge and Astra 600s PASS. Receipt docs/receipts/LEARNING_20261006_N049_ASTRA2.json.
