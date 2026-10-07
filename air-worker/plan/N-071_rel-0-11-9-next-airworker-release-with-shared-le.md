---
id: "N-071_rel-0-11-9-next-airworker-release-with-shared-le"
title: "REL-0.11.9: next AirWorker release with shared learning 0.1 as a subtask"
parent: "PLAN.md"
trigger: "LPR 07.10.2026: основной продукт — AirWorker; должен быть выпущен новый релиз AirWorker, а общий модуль самообучения является одной из задач этого релиза, не отдельной конечной целью. Сверяться с общим PLAN AirWorker и действовать."
owner: "gpt-airworker-release-20261007"
done_when: "Release AirWorker 0.11.9 as the next integrated product release after live 0.11.8. Treat N-070/shared air-modules learning 0.1.0 as a required subtask: finish its full regression/vet and independent semantic PASS, publish exact learning 0.1.0 only through its exact publication gate, then replace AirWorker commit pin with the released module tag/version and prove Go/sidecar consumer contracts. Reconcile stale open AirWorker learning nodes N-028..N-037 against actual 0.11.8 receipts and close/supersede only by factual evidence; include remaining relevant product debt rather than shipping a module-only release. Update product instruction, user-facing functional-capability description/help and changelog for the complete 0.11.9 behavior. Build a clean exact 0.11.9 candidate, run full regression/vet/native selfcheck/clean-session/shared-learning E2E and independent gpt-6-astra high, record rollback to 0.11.8 and exact binary/package hashes. Tag/GitHub release, marketplace promotion and live install are separate exact LPR gates; no reboot/UAC/foreign-service/ASW/bridge action."
status: "open"
return_to: "PLAN.md"
created_at: "2026-10-07T13:51:16.0381306Z"
updated_at: "2026-10-07T13:51:16.0381306Z"
receipts:
---

# REL-0.11.9: next AirWorker release with shared learning 0.1 as a subtask

- Родитель нити: PLAN.md
- Владелец: gpt-airworker-release-20261007
- Готово когда: Release AirWorker 0.11.9 as the next integrated product release after live 0.11.8. Treat N-070/shared air-modules learning 0.1.0 as a required subtask: finish its full regression/vet and independent semantic PASS, publish exact learning 0.1.0 only through its exact publication gate, then replace AirWorker commit pin with the released module tag/version and prove Go/sidecar consumer contracts. Reconcile stale open AirWorker learning nodes N-028..N-037 against actual 0.11.8 receipts and close/supersede only by factual evidence; include remaining relevant product debt rather than shipping a module-only release. Update product instruction, user-facing functional-capability description/help and changelog for the complete 0.11.9 behavior. Build a clean exact 0.11.9 candidate, run full regression/vet/native selfcheck/clean-session/shared-learning E2E and independent gpt-6-astra high, record rollback to 0.11.8 and exact binary/package hashes. Tag/GitHub release, marketplace promotion and live install are separate exact LPR gates; no reboot/UAC/foreign-service/ASW/bridge action.
