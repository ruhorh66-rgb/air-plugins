---
id: "N-056_recovery-inventory-airworker-wiki-sql-learning-r"
title: "RECOVERY-INVENTORY: AirWorker wiki SQL learning rules grants isolated verification"
parent: "post-0.11.7 AirWorker data integrity"
trigger: "LPR 06.10.2026 stage 3: after Vera recovery verification, perform analogous AirWorker inventory and agreed copy/isolated restore of wiki/product SQL/learning, rules and grants; source and working runtime unchanged."
owner: "gpt-airworker-handoff-20261006"
done_when: "Identify real source paths and consistency contract for AirWorker wiki, product SQL/state, learning, rules and grants; create only an isolated temporary copy/restore on R: without changing source, live runtime, registry or services; verify hashes/counts/schema/integrity and cross-file consistency; record exact source/readback and immutable receipt. No live migration, registry switch, install, ASW/bridge action or secret disclosure. If a consistent copy requires stopping a working service, stop before that action and leave an explicit LPR gate with what/why/rollback/check. Receipt docs/receipts/AIRWORKER_ISOLATED_RECOVERY_20261006.json."
status: "open"
return_to: "PLAN.md"
created_at: "2026-10-06T11:41:46.3700698Z"
updated_at: "2026-10-06T11:41:46.3700698Z"
receipts:
---

# RECOVERY-INVENTORY: AirWorker wiki SQL learning rules grants isolated verification

- Родитель нити: post-0.11.7 AirWorker data integrity
- Владелец: gpt-airworker-handoff-20261006
- Готово когда: Identify real source paths and consistency contract for AirWorker wiki, product SQL/state, learning, rules and grants; create only an isolated temporary copy/restore on R: without changing source, live runtime, registry or services; verify hashes/counts/schema/integrity and cross-file consistency; record exact source/readback and immutable receipt. No live migration, registry switch, install, ASW/bridge action or secret disclosure. If a consistent copy requires stopping a working service, stop before that action and leave an explicit LPR gate with what/why/rollback/check. Receipt docs/receipts/AIRWORKER_ISOLATED_RECOVERY_20261006.json.
