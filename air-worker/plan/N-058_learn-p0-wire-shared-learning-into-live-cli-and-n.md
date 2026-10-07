---
id: "N-058_learn-p0-wire-shared-learning-into-live-cli-and-n"
title: "LEARN-P0: wire shared learning into live CLI and next-run lifecycle"
parent: "Self-learning Hermes parity 20261005"
trigger: "LPR 06.10.2026: self-learning is the burning priority; install and update caches as soon as the learning block is actually ready. Current 0.11.6 and exact 0.11.7 candidate both return unknown learn action status, so CLI/lifecycle wiring is not complete."
owner: "gpt-airworker-handoff-20261006"
done_when: "Shared learning is reachable through the shipped air-worker CLI and ordinary lifecycle, with one configured state owner, status/paths/event/finalize/context/load/propose/pending/diff/approve/apply/rollback/effect/summary as supported by the module contract; completed run triggers bounded review; next run loads exact learned skill SHA and records loaded/used/outcome separately; grant-gated executable rules remain fail-closed; Go/subprocess regression and independent judge PASS. Then prepare exact release publication gate; after publication use the LPR 06.10.2026 explicit install/cache-update approval to install from GitHub and refresh Claude/Codex caches, with rollback and live smoke. No reboot/UAC/foreign-service/ASW/bridge action."
status: "closed"
return_to: "Self-learning Hermes parity 20261005"
created_at: "2026-10-06T14:50:37.5995939Z"
updated_at: "2026-10-07T10:51:40.0640578Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.7_SELF_LEARNING_CANDIDATE_20261007.json"
---

# LEARN-P0: wire shared learning into live CLI and next-run lifecycle

- Родитель нити: Self-learning Hermes parity 20261005
- Владелец: gpt-airworker-handoff-20261006
- Готово когда: Shared learning is reachable through the shipped air-worker CLI and ordinary lifecycle, with one configured state owner, status/paths/event/finalize/context/load/propose/pending/diff/approve/apply/rollback/effect/summary as supported by the module contract; completed run triggers bounded review; next run loads exact learned skill SHA and records loaded/used/outcome separately; grant-gated executable rules remain fail-closed; Go/subprocess regression and independent judge PASS. Then prepare exact release publication gate; after publication use the LPR 06.10.2026 explicit install/cache-update approval to install from GitHub and refresh Claude/Codex caches, with rollback and live smoke. No reboot/UAC/foreign-service/ASW/bridge action.
