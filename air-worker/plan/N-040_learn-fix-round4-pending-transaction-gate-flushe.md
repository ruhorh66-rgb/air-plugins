---
id: "N-040_learn-fix-round4-pending-transaction-gate-flushe"
title: "LEARN-FIX round4: pending transaction gate, flushed intents, bounded input"
parent: "N-038/N-035"
trigger: "LPR 2026-10-05: continue fixing Astra findings and prepare release; release authorized after checks in N-037; no installation or live blocking grant."
owner: "gpt-airworker-learning-20261005"
done_when: "All review-4 findings fixed in current consumer: under the product plan lock conflicting prepared new/migrate/close intents refuse other mutations before IDs or events; separate-process interleaving tests cover a partial migration and an unpublished ID reservation. Intent writes flush data before platform atomic publication and propagate flush/publication failure; no plan/event mutation on failed preparation. Oversized generated adapter input never launches process; summary stays pending_delivery with explicit error. Negative tests first fail against previous source, full Go tests/vet/native validation and independent Codex Astra PASS on exact manifest; record limits of power-loss proof and remaining live-learning gates. Receipt docs/receipts/LEARNING_20261005_ROUND4.json."
status: "closed"
return_to: "N-035"
created_at: "2026-10-05T13:25:29.1983151Z"
updated_at: "2026-10-05T14:14:16.739242Z"
receipts:
  - "docs/receipts/LEARNING_20261005_ROUND4.json"
---

# LEARN-FIX round4: pending transaction gate, flushed intents, bounded input

- Родитель нити: N-038/N-035
- Владелец: gpt-airworker-learning-20261005
- Готово когда: All review-4 findings fixed in current consumer: under the product plan lock conflicting prepared new/migrate/close intents refuse other mutations before IDs or events; separate-process interleaving tests cover a partial migration and an unpublished ID reservation. Intent writes flush data before platform atomic publication and propagate flush/publication failure; no plan/event mutation on failed preparation. Oversized generated adapter input never launches process; summary stays pending_delivery with explicit error. Negative tests first fail against previous source, full Go tests/vet/native validation and independent Codex Astra PASS on exact manifest; record limits of power-loss proof and remaining live-learning gates. Receipt docs/receipts/LEARNING_20261005_ROUND4.json.
