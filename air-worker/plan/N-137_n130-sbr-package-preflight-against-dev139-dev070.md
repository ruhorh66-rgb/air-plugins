---
id: "N-137_n130-sbr-package-preflight-against-dev139-dev070"
title: "N130 SBR package preflight against DEV139 DEV070 install registry"
parent: "N-130_rc4-release-p0-source-reviewer-fixes-and-transac"
trigger: "LPR 07.11.2026: check installer requirements in AIR OS standards, continue release preparation without repeating previous QA; current installed rc2 protected"
owner: "Dev Work 28 / SRVLM01"
done_when: "Read active DEV139/DEV070/DEV135/DEV130/DEV137 and installed-program registration requirement; prove AirWorker host-native portable format applicability vs Inno/MSI, check ARP/current installed version and rollback; create one isolated S source -> B installable package from exact GitHub HEAD without publication/install; verify embedded VCS S, B code tree identity, package CLI/tray SHA, manifests and signed payload preconditions, mark gaps and fail closed, machine receipt and independent source/code QA; plan native PASS close only with exact source SHA, package candidate hash and Git clean receipts; future publication/install remain separate LPR gate"
status: "closed"
return_to: "N-130_rc4-release-p0-source-reviewer-fixes-and-transac"
created_at: "2026-11-07T00:34:07.141343Z"
updated_at: "2026-11-07T00:41:13.2278455Z"
receipts:
  - "docs/receipts/DEVWORK28_N137_INSTALLER_STANDARDS_SBR_PACKAGE_PROOF_20261107.json"
---

# N130 SBR package preflight against DEV139 DEV070 install registry

- Родитель нити: N-130_rc4-release-p0-source-reviewer-fixes-and-transac
- Владелец: Dev Work 28 / SRVLM01
- Готово когда: Read active DEV139/DEV070/DEV135/DEV130/DEV137 and installed-program registration requirement; prove AirWorker host-native portable format applicability vs Inno/MSI, check ARP/current installed version and rollback; create one isolated S source -> B installable package from exact GitHub HEAD without publication/install; verify embedded VCS S, B code tree identity, package CLI/tray SHA, manifests and signed payload preconditions, mark gaps and fail closed, machine receipt and independent source/code QA; plan native PASS close only with exact source SHA, package candidate hash and Git clean receipts; future publication/install remain separate LPR gate
