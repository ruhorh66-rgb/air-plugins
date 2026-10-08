---
id: "N-083_p1-wal-recover-managed-procedure-transaction-bef"
title: "P1-WAL: recover managed procedure transaction before seed verification"
parent: "N-078_p0-self-owner-astra-remediate-seven-release-bloc"
trigger: "ЛПР 08.10.2026 после Astra high CHANGES_REQUIRED 4xP1: дорабатывай план, продолжай работу."
owner: "gpt-airworker-release-20261008"
done_when: "Fix Astra P1 bootstrapping after update/rollback crash: use module-owned recoverTransactions under proper locks before checking ledger-derived managed state, without touching immutable seed provenance or masking mismatched malicious files. Add negative post-bootstrap crash fixtures after target write before ledger append for update and rollback; fresh init/status/hook readback and exact journal verify; validate no direct LEARN edits. Full regression/vet, immutable receipts, independent Astra high PASS; no release/install."
status: "closed"
return_to: "N-078_p0-self-owner-astra-remediate-seven-release-bloc"
created_at: "2026-10-08T11:53:55.7529998Z"
updated_at: "2026-10-08T15:01:14.1106005Z"
receipts:
  - "R:\\-4-\\air-worker\\work\\0.11.9\\n090\\N078_ACCEPTANCE_bc33045.json"
---

# P1-WAL: recover managed procedure transaction before seed verification

- Родитель нити: N-078_p0-self-owner-astra-remediate-seven-release-bloc
- Владелец: gpt-airworker-release-20261008
- Готово когда: Fix Astra P1 bootstrapping after update/rollback crash: use module-owned recoverTransactions under proper locks before checking ledger-derived managed state, without touching immutable seed provenance or masking mismatched malicious files. Add negative post-bootstrap crash fixtures after target write before ledger append for update and rollback; fresh init/status/hook readback and exact journal verify; validate no direct LEARN edits. Full regression/vet, immutable receipts, independent Astra high PASS; no release/install.
