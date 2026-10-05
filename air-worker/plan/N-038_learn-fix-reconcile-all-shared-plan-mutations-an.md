---
id: "N-038_learn-fix-reconcile-all-shared-plan-mutations-an"
title: "LEARN-FIX: reconcile all shared plan mutations and one node snapshot"
parent: "N-030/N-035"
trigger: "LPR 2026-10-05 continuation and release after checks; Astra review-3 factual follow-up."
owner: "gpt-airworker-learning-20261005"
done_when: "Astra review-3 P1/P2: shared node new and migrate retain durable original IDs and a partially delivered event batch; explicit stable request-id defines retriable create/migrate, absent or reused-with-different-request IDs fail before mutation. Close Node/Body/BeforeSHA derive from one byte snapshot and a change during preflight is preserved/refused. Actual separate consumer subprocess retries cover new/migrate/close; fault injection occurs after event persistence. Go regression and independent Astra PASS on exact files; no release or stable claim while open. Receipt docs/receipts/LEARNING_20261005_PLAN_TRANSACTIONS.json."
status: "closed"
return_to: "N-035"
created_at: "2026-10-05T12:45:35.1594223Z"
updated_at: "2026-10-05T14:20:39.4823211Z"
receipts:
  - "docs/receipts/LEARNING_20261005_PLAN_TRANSACTIONS.json"
---

# LEARN-FIX: reconcile all shared plan mutations and one node snapshot

- Родитель нити: N-030/N-035
- Владелец: gpt-airworker-learning-20261005
- Готово когда: Astra review-3 P1/P2: shared node new and migrate retain durable original IDs and a partially delivered event batch; explicit stable request-id defines retriable create/migrate, absent or reused-with-different-request IDs fail before mutation. Close Node/Body/BeforeSHA derive from one byte snapshot and a change during preflight is preserved/refused. Actual separate consumer subprocess retries cover new/migrate/close; fault injection occurs after event persistence. Go regression and independent Astra PASS on exact files; no release or stable claim while open. Receipt docs/receipts/LEARNING_20261005_PLAN_TRANSACTIONS.json.
