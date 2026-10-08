---
id: "N-074_recovery-volt-align-airworker-product-vault-with"
title: "RECOVERY-VOLT: align AirWorker product vault with active 0.11.9 line"
parent: "N-072_recovery-0-11-9-restore-one-authoritative-develo"
trigger: "LPR 08.10.2026 approved post-incident reconciliation; audit found AirWorker_Wiki PRODUCT_INSTRUCTION restored to 05.10 and pointing to an obsolete development worktree, while a newer 08.10 learning backlog is untracked."
owner: "gpt-airworker-release-20261008"
done_when: "Update AirWorker_Wiki PRODUCT_INSTRUCTION to the verified active 0.11.9 authority and recovery entrypoint, preserve the historical 05.10 handoff as history, commit the 08.10 learning backlog, explicitly distinguish AirWorker self-learning P0 from AirCurator/product-consumer learning, and record a read-back receipt. Do not claim live cutover or install. Commit and push the product-vault branch after verification."
status: "closed"
return_to: "N-071_rel-0-11-9-next-airworker-release-with-shared-le"
created_at: "2026-10-08T03:41:01.8600478Z"
updated_at: "2026-10-08T03:43:27.7700537Z"
receipts:
  - "docs/receipts/AIRWORKER_RECOVERY_VAULT_20261008.json"
---

# RECOVERY-VOLT: align AirWorker product vault with active 0.11.9 line

- Родитель нити: N-072_recovery-0-11-9-restore-one-authoritative-develo
- Владелец: gpt-airworker-release-20261008
- Готово когда: Update AirWorker_Wiki PRODUCT_INSTRUCTION to the verified active 0.11.9 authority and recovery entrypoint, preserve the historical 05.10 handoff as history, commit the 08.10 learning backlog, explicitly distinguish AirWorker self-learning P0 from AirCurator/product-consumer learning, and record a read-back receipt. Do not claim live cutover or install. Commit and push the product-vault branch after verification.
