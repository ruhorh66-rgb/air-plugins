---
id: "N-072_recovery-0-11-9-restore-one-authoritative-develo"
title: "RECOVERY-0.11.9: restore one authoritative development line after F incident"
parent: "N-071_rel-0-11-9-next-airworker-release-with-shared-le"
trigger: "LPR 08.10.2026 approved recovery plan and explicitly required that the recovery measures themselves be written into the AirWorker development PLAN."
owner: "gpt-airworker-release-20261008"
done_when: "1) Preserve after-restore WIP/checkpoints before mutation. 2) Establish F:\\-7-\\_worktrees\\airworker-curator-skill-lifecycle-20261006 at saved HEAD 8826771 as the sole active AirWorker 0.11.9 development line; keep older F:\\-7-\\air-worker and airworker-learning-20261005 as historical/recovery sources only. 3) Add and execute the P0 permanent AirWorker self-learning owner/runtime subtask. 4) Reconcile AirWorker_Wiki PRODUCT_INSTRUCTION and backlog with the active 0.11.9 PLAN while preserving history. 5) Restore shared air-modules learning work from GitHub authoritative branch without destroying recovered local WIP, verify exact commits/receipts, and continue it only as a subtask of 0.11.9. 6) Commit and push recovery state to protected development branches. No release/tag/install/reboot/UAC/foreign-service/ASW/bridge action in this node."
status: "open"
return_to: "N-071_rel-0-11-9-next-airworker-release-with-shared-le"
created_at: "2026-10-08T03:40:31.3686974Z"
updated_at: "2026-10-08T03:40:31.3686974Z"
receipts:
---

# RECOVERY-0.11.9: restore one authoritative development line after F incident

- Родитель нити: N-071_rel-0-11-9-next-airworker-release-with-shared-le
- Владелец: gpt-airworker-release-20261008
- Готово когда: 1) Preserve after-restore WIP/checkpoints before mutation. 2) Establish F:\-7-\_worktrees\airworker-curator-skill-lifecycle-20261006 at saved HEAD 8826771 as the sole active AirWorker 0.11.9 development line; keep older F:\-7-\air-worker and airworker-learning-20261005 as historical/recovery sources only. 3) Add and execute the P0 permanent AirWorker self-learning owner/runtime subtask. 4) Reconcile AirWorker_Wiki PRODUCT_INSTRUCTION and backlog with the active 0.11.9 PLAN while preserving history. 5) Restore shared air-modules learning work from GitHub authoritative branch without destroying recovered local WIP, verify exact commits/receipts, and continue it only as a subtask of 0.11.9. 6) Commit and push recovery state to protected development branches. No release/tag/install/reboot/UAC/foreign-service/ASW/bridge action in this node.
