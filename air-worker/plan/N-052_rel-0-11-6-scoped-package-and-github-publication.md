---
id: "N-052_rel-0-11-6-scoped-package-and-github-publication"
title: "REL-0.11.6: scoped package and GitHub publication"
parent: "N-037"
trigger: "N-037 LPR authorization: publish a new release after verified checks; fast-path 2026-10-06 excludes concurrent curator-only skill commits from the scoped hardening release."
owner: "gpt-airworker-learning-20261005"
done_when: "Create a clean release candidate from accepted AirWorker hardening without fbc76de/68ca8ef/08425c3/a47a8e8 curator-only skill commits; bump all canonical package/runtime manifests to 0.11.6 and add scoped changelog. Build binaries from a clean checkout with vcs.revision matching the exact release source and vcs.modified=false. Run full Go/vet, native plan-lint/goals/validate, factual judge and independent gpt-6-astra high 600s PASS on exact release source/package. Verify rollback availability and identity for installed 0.11.5 without installing anything. Push reviewed source/package, tag air-worker--v0.11.6 and publish GitHub Release from the checked package; read back tag/assets/SHA. Do not install/cut over/reboot/UAC/stop services. Receipt docs/receipts/LEARNING_20261006_RELEASE_0_11_6.json."
status: "open"
return_to: "N-037"
created_at: "2026-10-06T04:44:21.0066709Z"
updated_at: "2026-10-06T04:44:21.0066709Z"
receipts:
---

# REL-0.11.6: scoped package and GitHub publication

- Родитель нити: N-037
- Владелец: gpt-airworker-learning-20261005
- Готово когда: Create a clean release candidate from accepted AirWorker hardening without fbc76de/68ca8ef/08425c3/a47a8e8 curator-only skill commits; bump all canonical package/runtime manifests to 0.11.6 and add scoped changelog. Build binaries from a clean checkout with vcs.revision matching the exact release source and vcs.modified=false. Run full Go/vet, native plan-lint/goals/validate, factual judge and independent gpt-6-astra high 600s PASS on exact release source/package. Verify rollback availability and identity for installed 0.11.5 without installing anything. Push reviewed source/package, tag air-worker--v0.11.6 and publish GitHub Release from the checked package; read back tag/assets/SHA. Do not install/cut over/reboot/UAC/stop services. Receipt docs/receipts/LEARNING_20261006_RELEASE_0_11_6.json.
