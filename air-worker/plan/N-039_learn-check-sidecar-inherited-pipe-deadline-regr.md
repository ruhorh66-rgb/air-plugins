---
id: "N-039_learn-check-sidecar-inherited-pipe-deadline-regr"
title: "LEARN-CHECK: sidecar inherited-pipe deadline regression"
parent: "N-035"
trigger: "LPR 2026-10-05 continuation and release after checks; Astra review-3 factual follow-up."
owner: "gpt-airworker-learning-20261005"
done_when: "Fresh full shared-module regression returned exit 1: TestAstraSidecarInheritedPipeCannotDefeatDeadline measured 736ms against 600ms while direct sidecar context was 100ms and WaitDelay 250ms. Diagnose under controlled serial load, distinguish startup/scheduler delay from inherited-pipe leak, retain the failing receipt, and require an evidence-backed test/implementation fix if repeatable. Do not erase the failure by an unexplained timeout increase. Module/source identity and a complete regression receipt required before release. Receipt docs/receipts/LEARNING_20261005_SIDECAR_DEADLINE.json."
status: "open"
return_to: "N-035"
created_at: "2026-10-05T12:45:35.2100394Z"
updated_at: "2026-10-05T12:45:35.2100394Z"
receipts:
---

# LEARN-CHECK: sidecar inherited-pipe deadline regression

- Родитель нити: N-035
- Владелец: gpt-airworker-learning-20261005
- Готово когда: Fresh full shared-module regression returned exit 1: TestAstraSidecarInheritedPipeCannotDefeatDeadline measured 736ms against 600ms while direct sidecar context was 100ms and WaitDelay 250ms. Diagnose under controlled serial load, distinguish startup/scheduler delay from inherited-pipe leak, retain the failing receipt, and require an evidence-backed test/implementation fix if repeatable. Do not erase the failure by an unexplained timeout increase. Module/source identity and a complete regression receipt required before release. Receipt docs/receipts/LEARNING_20261005_SIDECAR_DEADLINE.json.
