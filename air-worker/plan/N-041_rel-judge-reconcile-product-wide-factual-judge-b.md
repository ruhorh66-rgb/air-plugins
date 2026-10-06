---
id: "N-041_rel-judge-reconcile-product-wide-factual-judge-b"
title: "REL-JUDGE: reconcile product-wide factual judge before release"
parent: "LEARN-REL"
trigger: "LPR 2026-10-05: разбирайся с долгами, сверяйся с планом и продолжай; factual judge 2026-10-05 returned code 1 distance 33 after N-040 fixes."
owner: "gpt-airworker-learning-20261005"
done_when: "On the exact release candidate, every current PLAN criterion has a real runnable check or accepted fact; stale Criterion*Pending selectors are mapped to the actual existing test or removed only when the criterion is historically superseded with evidence; Hermes plugin check has a diagnosed/fixed root cause; goal-drift has a fresh factual verdict; air-worker judge -json completes with exit code 0. Full Go regression and plan-lint/validate PASS; independent Astra reviews the judge/plan reconciliation. Receipt docs/receipts/LEARNING_20261005_GLOBAL_JUDGE.json."
status: "closed"
return_to: "N-036"
created_at: "2026-10-05T14:23:15.7370107Z"
updated_at: "2026-10-06T02:29:43.2841946Z"
receipts:
  - "docs/receipts/LEARNING_20261005_GLOBAL_JUDGE.json"
---

# REL-JUDGE: reconcile product-wide factual judge before release

- Родитель нити: LEARN-REL
- Владелец: gpt-airworker-learning-20261005
- Готово когда: On the exact release candidate, every current PLAN criterion has a real runnable check or accepted fact; stale Criterion*Pending selectors are mapped to the actual existing test or removed only when the criterion is historically superseded with evidence; Hermes plugin check has a diagnosed/fixed root cause; goal-drift has a fresh factual verdict; air-worker judge -json completes with exit code 0. Full Go regression and plan-lint/validate PASS; independent Astra reviews the judge/plan reconciliation. Receipt docs/receipts/LEARNING_20261005_GLOBAL_JUDGE.json.
