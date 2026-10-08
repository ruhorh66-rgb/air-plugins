---
id: "N-094_p1-learn-cli-feedback-shared-learning-cli-discov"
title: "P1-LEARN-CLI-FEEDBACK shared-learning CLI discovery and canonical feedback intake parity"
parent: "N-076_p0-self-owner-persistent-airworker-selector-dual"
trigger: "08.10.2026 real AirVera development session on installed AirWorker 0.11.8: shared-learning event -> reviewer -> safe procedure auto-apply -> context load by exact SHA works; however air-worker learn --help and unqualified learn status/paths return unknown action despite root help advertising them, and air-worker feedback in shared mode produced EV-4da418f373d6e032b40ef9c5 plus auto-applied feedback-error-a0309df4.md but did not create a new .air-worker/feedback/FB-*.json or PLAN feedback candidate as the public feedback contract promises."
owner: "gpt-airworker-release-20261008"
done_when: "Keep the working shared-learning chain unchanged and add regression evidence for event -> production reviewer -> safe procedure auto-apply -> pending=0 -> context/load by exact target@SHA. Make learn CLI self-consistent: learn --help and learn <action> --help expose the real shared/legacy command contract, recognized shared actions without -product return a typed product-required diagnostic rather than unknown action, and root help/no-args/action help are tested from one contract. Preserve canonical product feedback intake in shared mode: air-worker feedback must still create immutable .air-worker/feedback/FB-*.json evidence plus a non-executable canonical PLAN/backlog candidate (or one documented equivalent canonical record) while it may also feed shared learning; auto-applying a safe learned procedure must never replace developer feedback/backlog evidence. Add negative tests for duplicate intake, malformed/missing product, shared-mode routing and authority non-escalation. Do not duplicate N-076/N-093 P0 automatic capture/use/effect scope."
status: "open"
return_to: "N-076_p0-self-owner-persistent-airworker-selector-dual"
created_at: "2026-10-08T15:30:58.9701602Z"
updated_at: "2026-10-08T15:30:58.9701602Z"
receipts:
---

# P1-LEARN-CLI-FEEDBACK shared-learning CLI discovery and canonical feedback intake parity

- Родитель нити: N-076_p0-self-owner-persistent-airworker-selector-dual
- Владелец: gpt-airworker-release-20261008
- Готово когда: Keep the working shared-learning chain unchanged and add regression evidence for event -> production reviewer -> safe procedure auto-apply -> pending=0 -> context/load by exact target@SHA. Make learn CLI self-consistent: learn --help and learn <action> --help expose the real shared/legacy command contract, recognized shared actions without -product return a typed product-required diagnostic rather than unknown action, and root help/no-args/action help are tested from one contract. Preserve canonical product feedback intake in shared mode: air-worker feedback must still create immutable .air-worker/feedback/FB-*.json evidence plus a non-executable canonical PLAN/backlog candidate (or one documented equivalent canonical record) while it may also feed shared learning; auto-applying a safe learned procedure must never replace developer feedback/backlog evidence. Add negative tests for duplicate intake, malformed/missing product, shared-mode routing and authority non-escalation. Do not duplicate N-076/N-093 P0 automatic capture/use/effect scope.
