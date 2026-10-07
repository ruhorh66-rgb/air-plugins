---
id: "N-069_rel-0-11-8-live-publish-lf-hotfix-and-replace-0-1"
title: "REL-0.11.8-LIVE: publish LF hotfix and replace 0.11.7"
parent: "N-067_live-0-11-7-promote-marketplace-install-refresh-c"
trigger: "Live 0.11.7 install/cache refresh exposed canonical marketplace CRLF checkout drift: selfcheck RED on all three release-owned AirCurator skill raw SHA, while CRLF-to-LF hashes matched registry exactly. Exact 0.11.8 candidate fixes checkout EOL via scoped gitattributes and passed core.autocrlf=true raw SHA/package-health, full Go/vet, clean SessionStart, shared-learning regression and independent gpt-6-astra high PASS."
owner: "gpt-airworker-handoff-20261007"
done_when: "Only after explicit LPR yes naming source cbfa266eefe8d5d1ea67db078bc416f4a928147a and package e1213a6d71e1ccdd3d487e244cf5ad21d9d1e9ae: publish tag/GitHub Release air-worker--v0.11.8 from exact package without rebuild; fast-forward marketplace main to exact package; verify downloaded assets and binary SHA CLI 1B4008AF05110214934D46C0AB8C5236341CD1C699F54EFB895C2F9F64A94A94 / tray F9907BB81F104E412F743B40F56AAA669C12015841E30CC17A9D7F8FAE84465E. Run official Claude/Codex marketplace refresh and require raw AirCurator skill SHA health PASS under Windows checkout. Install non-elevated only from canonical GitHub cache, verify live selfcheck green, one current tray, and installed-binary isolated shared-learning smoke. Rollback target first 0.11.7 CLI SHA C3709D1495240C3423316D3459D7A185B644877F270DABF570911E4B767F63E3, fallback 0.11.6 SHA 3561965DEEB98AB375889C3F78CF9E86ED8F9D9763ECE305ECB655CAC73FD4A9. No reboot/UAC/foreign-service/ASW/bridge action."
status: "closed"
return_to: "N-067_live-0-11-7-promote-marketplace-install-refresh-c"
created_at: "2026-10-07T12:41:12.8520364Z"
updated_at: "2026-10-07T13:02:28.2399417Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.8_LIVE_20261007.json"
---

# REL-0.11.8-LIVE: publish LF hotfix and replace 0.11.7

- Родитель нити: N-067_live-0-11-7-promote-marketplace-install-refresh-c
- Владелец: gpt-airworker-handoff-20261007
- Готово когда: Only after explicit LPR yes naming source cbfa266eefe8d5d1ea67db078bc416f4a928147a and package e1213a6d71e1ccdd3d487e244cf5ad21d9d1e9ae: publish tag/GitHub Release air-worker--v0.11.8 from exact package without rebuild; fast-forward marketplace main to exact package; verify downloaded assets and binary SHA CLI 1B4008AF05110214934D46C0AB8C5236341CD1C699F54EFB895C2F9F64A94A94 / tray F9907BB81F104E412F743B40F56AAA669C12015841E30CC17A9D7F8FAE84465E. Run official Claude/Codex marketplace refresh and require raw AirCurator skill SHA health PASS under Windows checkout. Install non-elevated only from canonical GitHub cache, verify live selfcheck green, one current tray, and installed-binary isolated shared-learning smoke. Rollback target first 0.11.7 CLI SHA C3709D1495240C3423316D3459D7A185B644877F270DABF570911E4B767F63E3, fallback 0.11.6 SHA 3561965DEEB98AB375889C3F78CF9E86ED8F9D9763ECE305ECB655CAC73FD4A9. No reboot/UAC/foreign-service/ASW/bridge action.
