---
id: "N-025_0-11-3-align-planner-release-check-with-canonica"
title: "0.11.3 align planner release check with canonical plan thread"
parent: "N-019 0.11.3 release regression"
trigger: "Final package gate: check-planner requires historical .woody/planner-answer.json and treats approved legacy plan tiers haiku/sonnet/opus as outside the 0.11.3 GPT-6 ladder."
owner: "AC·DEV·AirWorker"
done_when: "check-planner validates the current binary planner contract without requiring an old local planner receipt: canonical PLAN/plan-node thread is present, existing PLAN.md remains non-overwritable, proposed-plan grammar is checked when a proposal exists, and legacy tier aliases are validated through the 0.11.3 mapping; check returns zero and package regression continues."
status: "closed"
return_to: "N-019 0.11.3 release regression"
created_at: "2026-09-29T11:30:24.2566235Z"
updated_at: "2026-09-29T12:44:29.8124011Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.3_PACKAGE_RELEASE_ACCEPTANCE_20260929.md"
---

# 0.11.3 align planner release check with canonical plan thread

- Родитель нити: N-019 0.11.3 release regression
- Владелец: AC·DEV·AirWorker
- Готово когда: check-planner validates the current binary planner contract without requiring an old local planner receipt: canonical PLAN/plan-node thread is present, existing PLAN.md remains non-overwritable, proposed-plan grammar is checked when a proposal exists, and legacy tier aliases are validated through the 0.11.3 mapping; check returns zero and package regression continues.
