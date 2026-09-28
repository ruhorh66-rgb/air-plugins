---
id: "N-009_0-11-2-refresh-factual-judge-on-commit-and-tick"
title: "0.11.2: refresh factual judge on commit and tick"
parent: "N-001 complete self-learning loop"
trigger: "Сверка L11-2: freshness detector реализован, но commit/tick auto-refresh отсутствует."
owner: "AC·DEV·AirWorker"
done_when: "After a successful worker git commit and during curator tick, a missing verdict, verdict older than 6h, changed git HEAD or changed judge fingerprint is rerun and republished by core; fresh verdict is reused; unmeasurable product is explicit NOT_PROVEN/fail-closed."
status: "closed"
return_to: "N-001 self-learning acceptance"
created_at: "2026-09-28T19:47:21.8247464Z"
updated_at: "2026-09-28T20:00:34.5946507Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.2_JUDGE_AUTO_REFRESH_ACCEPTANCE_20260928.md"
---

# 0.11.2: refresh factual judge on commit and tick

- Родитель нити: N-001 complete self-learning loop
- Владелец: AC·DEV·AirWorker
- Готово когда: After a successful worker git commit and during curator tick, a missing verdict, verdict older than 6h, changed git HEAD or changed judge fingerprint is rerun and republished by core; fresh verdict is reused; unmeasurable product is explicit NOT_PROVEN/fail-closed.
