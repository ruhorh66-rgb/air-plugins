---
id: "N-022_0-11-3-remove-stale-llm-queue-release-check"
title: "0.11.3 remove stale llm-queue release check"
parent: "N-019 0.11.3 release regression"
trigger: "0.11.3 package gate: check-ladder-reachable still requires removed E:/-8-/llm-queue dispatcher although approved executor architecture is Codex/OpenAI + Ponytail and Anthropic judges via Headroom."
owner: "AC·DEV·AirWorker"
done_when: "check-ladder-reachable no longer depends on legacy llm-queue; it proves packaged AirWorker 0.11.3 resolves Ponytail, Headroom and approved executor list through current core, returns zero on SRVLM01, and full package regression passes."
status: "closed"
return_to: "N-019 0.11.3 release regression"
created_at: "2026-09-29T11:11:02.5663146Z"
updated_at: "2026-09-29T12:44:29.7592507Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.3_PACKAGE_RELEASE_ACCEPTANCE_20260929.md"
---

# 0.11.3 remove stale llm-queue release check

- Родитель нити: N-019 0.11.3 release regression
- Владелец: AC·DEV·AirWorker
- Готово когда: check-ladder-reachable no longer depends on legacy llm-queue; it proves packaged AirWorker 0.11.3 resolves Ponytail, Headroom and approved executor list through current core, returns zero on SRVLM01, and full package regression passes.
