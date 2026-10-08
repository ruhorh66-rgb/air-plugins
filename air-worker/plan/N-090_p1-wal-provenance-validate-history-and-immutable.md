---
id: "N-090_p1-wal-provenance-validate-history-and-immutable"
title: "P1-WAL-PROVENANCE: validate history and immutable preflight"
parent: "N-087_p1-wal-validate-reject-malicious-recovery-mutati"
trigger: "Independent GPT-6 Astra high CHANGES_REQUIRED on clean ef6f396 (2026-10-08); four P1 findings; continue existing N-078 plan per LPR."
owner: "gpt-airworker-release-20261008"
done_when: "Resolve Astra ef6f396 WAL P1 findings 1,2,3 before release. Under module-owned locks validate WAL proposer identity against accepted producer IDs (validID, not LP-only), authenticated proposal/history and transaction relationship, target pre/post snapshots, ledger records, no conflicting committed prior history; forbid forged WAL deleting existing managed skills. Validate ALL WALs and read-only canonical ledger prefix and target identities BEFORE any modification including recoverIncompleteLedgerTail. Preserve byte-identical seed, managed files, ledger and WAL on poison/conflict. Include negative forged existing procedure, valid custom-update proposal IDs for interrupted update/rollback, conflicting terminal tail, full module Go test/vet exact immutable commit, AirWorker repin full regression and independent Astra high PASS. No direct LEARN journal edit."
status: "open"
return_to: "N-087_p1-wal-validate-reject-malicious-recovery-mutati"
created_at: "2026-10-08T14:13:22.6160624Z"
updated_at: "2026-10-08T14:13:22.6160624Z"
receipts:
---

# P1-WAL-PROVENANCE: validate history and immutable preflight

- Родитель нити: N-087_p1-wal-validate-reject-malicious-recovery-mutati
- Владелец: gpt-airworker-release-20261008
- Готово когда: Resolve Astra ef6f396 WAL P1 findings 1,2,3 before release. Under module-owned locks validate WAL proposer identity against accepted producer IDs (validID, not LP-only), authenticated proposal/history and transaction relationship, target pre/post snapshots, ledger records, no conflicting committed prior history; forbid forged WAL deleting existing managed skills. Validate ALL WALs and read-only canonical ledger prefix and target identities BEFORE any modification including recoverIncompleteLedgerTail. Preserve byte-identical seed, managed files, ledger and WAL on poison/conflict. Include negative forged existing procedure, valid custom-update proposal IDs for interrupted update/rollback, conflicting terminal tail, full module Go test/vet exact immutable commit, AirWorker repin full regression and independent Astra high PASS. No direct LEARN journal edit.
