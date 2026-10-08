---
id: "N-086_p1-relock-abort-before-write-after-lost-reviewer"
title: "P1-RELOCK: abort before write after lost reviewer lock"
parent: "N-082_p1-lock-release-shared-learning-review-locks-bef"
trigger: "Independent GPT-6 Astra high verdict CHANGES_REQUIRED on clean c00c167 (2026-10-08), 4 P1; LPR instructed resume work under PLAN."
owner: "gpt-airworker-release-20261008"
done_when: "Astra c00c167 finding1: when judge/reviewer callback cannot reacquire target+state locks within deadline, no review/event/ledger/trace mutations happen without ownership and operation returns explicit nonzero. Add deterministic contention and relock-failure tests for Judge AND Reviewer, same-run lease and retry/idempotency. Module exact clean commit test ./... and vet PASS; consumer full suite and independent Astra high PASS. Preserve all other owner runs; release/install prohibited."
status: "open"
return_to: "N-082_p1-lock-release-shared-learning-review-locks-bef"
created_at: "2026-10-08T13:27:58.1758127Z"
updated_at: "2026-10-08T13:27:58.1758127Z"
receipts:
---

# P1-RELOCK: abort before write after lost reviewer lock

- Родитель нити: N-082_p1-lock-release-shared-learning-review-locks-bef
- Владелец: gpt-airworker-release-20261008
- Готово когда: Astra c00c167 finding1: when judge/reviewer callback cannot reacquire target+state locks within deadline, no review/event/ledger/trace mutations happen without ownership and operation returns explicit nonzero. Add deterministic contention and relock-failure tests for Judge AND Reviewer, same-run lease and retry/idempotency. Module exact clean commit test ./... and vet PASS; consumer full suite and independent Astra high PASS. Preserve all other owner runs; release/install prohibited.
