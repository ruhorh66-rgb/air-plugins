---
id: "N-023_0-11-3-align-engine-release-check-with-judgedeta"
title: "0.11.3 align engine release check with judgeDetailed loop"
parent: "N-019 0.11.3 release regression"
trigger: "Package gate: check-engine still counts only c.judge() calls, but current Go loop performs initial c.judge() then per-iteration c.judgeDetailed()."
owner: "AC·DEV·AirWorker"
done_when: "check-engine proves an initial factual judge plus judgeDetailed inside the internal Go loop without assuming duplicate c.judge() syntax; check-engine returns zero and full package regression remains green."
status: "closed"
return_to: "N-019 0.11.3 release regression"
created_at: "2026-09-29T11:15:49.5161307Z"
updated_at: "2026-09-29T12:44:29.7862541Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.3_PACKAGE_RELEASE_ACCEPTANCE_20260929.md"
---

# 0.11.3 align engine release check with judgeDetailed loop

- Родитель нити: N-019 0.11.3 release regression
- Владелец: AC·DEV·AirWorker
- Готово когда: check-engine proves an initial factual judge plus judgeDetailed inside the internal Go loop without assuming duplicate c.judge() syntax; check-engine returns zero and full package regression remains green.
