---
id: "N-066_rel-0-11-7-publish-exact-github-publication-gate"
title: "REL-0.11.7-PUBLISH: exact GitHub publication gate"
parent: "PLAN.md"
trigger: "Exact AirWorker 0.11.7 candidate PASS: source c78493bb485bda3f4ecb9d1d28a120400c9eb00f, package e867f3819c98d91d70bc4b498f415d608bc88b95, full regression/native/clean-session/self-learning E2E/curator negatives/gpt-6-astra high all PASS. Publication/tag/push remains a separate exact LPR gate."
owner: "gpt-airworker-handoff-20261007"
done_when: "Only after an explicit LPR yes naming source c78493bb485bda3f4ecb9d1d28a120400c9eb00f and package e867f3819c98d91d70bc4b498f415d608bc88b95: publish AirWorker 0.11.7 from that exact package without rebuild, create/verify tag and GitHub Release, verify CLI SHA256 C3709D1495240C3423316D3459D7A185B644877F270DABF570911E4B767F63E3 and tray SHA256 2683334329CB36B76CCA92DFEC31609D612E82F107BA9218E9DA444340077D6D against downloaded release assets, and record immutable publication receipt. Do not republish 0.11.6. After publication PASS, use the existing LPR install/cache-refresh authorization to install only from GitHub, with rollback to live 0.11.6 SHA 3561965DEEB98AB375889C3F78CF9E86ED8F9D9763ECE305ECB655CAC73FD4A9, refresh Claude/Codex caches, and run live self-learning smoke. No reboot/UAC/foreign-service/ASW/bridge action."
status: "closed"
return_to: "PLAN.md"
created_at: "2026-10-07T10:54:36.8533586Z"
updated_at: "2026-10-07T12:15:17.2841848Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.7_RELEASE_20261007.json"
---

# REL-0.11.7-PUBLISH: exact GitHub publication gate

- Родитель нити: PLAN.md
- Владелец: gpt-airworker-handoff-20261007
- Готово когда: Only after an explicit LPR yes naming source c78493bb485bda3f4ecb9d1d28a120400c9eb00f and package e867f3819c98d91d70bc4b498f415d608bc88b95: publish AirWorker 0.11.7 from that exact package without rebuild, create/verify tag and GitHub Release, verify CLI SHA256 C3709D1495240C3423316D3459D7A185B644877F270DABF570911E4B767F63E3 and tray SHA256 2683334329CB36B76CCA92DFEC31609D612E82F107BA9218E9DA444340077D6D against downloaded release assets, and record immutable publication receipt. Do not republish 0.11.6. After publication PASS, use the existing LPR install/cache-refresh authorization to install only from GitHub, with rollback to live 0.11.6 SHA 3561965DEEB98AB375889C3F78CF9E86ED8F9D9763ECE305ECB655CAC73FD4A9, refresh Claude/Codex caches, and run live self-learning smoke. No reboot/UAC/foreign-service/ASW/bridge action.
