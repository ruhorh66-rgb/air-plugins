---
id: "N-070_handoff-after-0-11-8-publish-shared-learning-0-1"
title: "HANDOFF: after 0.11.8 publish shared learning 0.1 from air-modules"
parent: "N-067_live-0-11-7-promote-marketplace-install-refresh-c"
trigger: "LPR 07.10.2026: after completing AirWorker 0.11.8, also publish the self-learning block as the shared module for other products; update the plan so an interrupted session knows exactly where and what to continue."
owner: "gpt-airworker-handoff-20261007"
done_when: "After exact AirWorker 0.11.8 live PASS, continue the same LPR priority in canonical repo F:\\-8-\\air-modules / GitHub ruhorh66-rgb/air-modules, package github.com/ruhorh66-rgb/air-modules/learning plus cmd/air-learn sidecar. Do not build a second learning engine in AirWorker or another product. First action after any interruption: verify SRVLM01 online, AirWorker HEAD/PLAN, air-modules HEAD/status/diff/processes/receipts, preserve existing dirty WIP and never reset/checkout over it; reconcile current WIP against AirWorker pinned module commit 2ca7e731607e and proven 0.11.8 behavior before mutation. Follow AIR_VIBECODING 2.0.0-rc.9 invariants 14-16: one shared learning owner, Git-managed procedures/rules on F:, runtime journal/reviews/ledger on R:, run_id and automatic review, exact-SHA next-run load/outcome, blocking changes only through trusted LPR grant, CLI help plus OPERATIONS recovery contract. Prepare and publish exact air-modules learning 0.1.0 for reuse by other AIR products with Go import and bundled air-learn sidecar, full tests/vet, consumer contract tests, rollback, independent judge and downloaded-asset/SHA verification. Record module release receipt and then pin AirWorker/other consumers to the released tag. No reboot/UAC/foreign-service/ASW/bridge actions."
status: "open"
return_to: "N-067_live-0-11-7-promote-marketplace-install-refresh-c"
created_at: "2026-10-07T12:50:09.8421326Z"
updated_at: "2026-10-07T12:50:09.8421326Z"
receipts:
---

# HANDOFF: after 0.11.8 publish shared learning 0.1 from air-modules

- Родитель нити: N-067_live-0-11-7-promote-marketplace-install-refresh-c
- Владелец: gpt-airworker-handoff-20261007
- Готово когда: After exact AirWorker 0.11.8 live PASS, continue the same LPR priority in canonical repo F:\-8-\air-modules / GitHub ruhorh66-rgb/air-modules, package github.com/ruhorh66-rgb/air-modules/learning plus cmd/air-learn sidecar. Do not build a second learning engine in AirWorker or another product. First action after any interruption: verify SRVLM01 online, AirWorker HEAD/PLAN, air-modules HEAD/status/diff/processes/receipts, preserve existing dirty WIP and never reset/checkout over it; reconcile current WIP against AirWorker pinned module commit 2ca7e731607e and proven 0.11.8 behavior before mutation. Follow AIR_VIBECODING 2.0.0-rc.9 invariants 14-16: one shared learning owner, Git-managed procedures/rules on F:, runtime journal/reviews/ledger on R:, run_id and automatic review, exact-SHA next-run load/outcome, blocking changes only through trusted LPR grant, CLI help plus OPERATIONS recovery contract. Prepare and publish exact air-modules learning 0.1.0 for reuse by other AIR products with Go import and bundled air-learn sidecar, full tests/vet, consumer contract tests, rollback, independent judge and downloaded-asset/SHA verification. Record module release receipt and then pin AirWorker/other consumers to the released tag. No reboot/UAC/foreign-service/ASW/bridge actions.
