---
id: "N-138_n130-windows-installed-program-registration-and-s"
title: "N130 Windows installed-program registration and safe removal lifecycle"
parent: "N-130_rc4-release-p0-source-reviewer-fixes-and-transac"
trigger: "Standards audit 07.11.2026: PRODUCTS_INSTALL_REGISTRATION requires AirWorker Windows installed-program entry, version upgrade, safe uninstall; HKCU/HKLM readback zero AirWorker entries, native install -uninstall leaves bin/ files; LPR requests installer standards compliance"
owner: "Dev Work 28 / SRVLM01"
done_when: "Do not replace existing portable host-native updater with unapproved MSI/Inno; implement per-user Windows installed-program identity in existing AirWorker installer only with owned HKCU ARP key, exact display version/publisher/location and a genuinely supported remove contract; update transaction changes registry only after successful CLI/tray and native Claude/Codex cache identity, failure/rollback restores previous ARP and version; uninstall preserves owner data and does not remove unrelated files, report incomplete owned-bin deletion honestly; isolated native registry fixture and upgrade/rollback/uninstall tests including duplicate/downgrade, no live registry mutation until approved release; factual Go/PE checks, independent judge, exact source SHA and receipt, N130 overall open until installed acceptance"
status: "open"
return_to: "N-130_rc4-release-p0-source-reviewer-fixes-and-transac"
created_at: "2026-11-07T00:41:36.7026392Z"
updated_at: "2026-11-07T00:41:36.7026392Z"
receipts:
---

# N130 Windows installed-program registration and safe removal lifecycle

- Родитель нити: N-130_rc4-release-p0-source-reviewer-fixes-and-transac
- Владелец: Dev Work 28 / SRVLM01
- Готово когда: Do not replace existing portable host-native updater with unapproved MSI/Inno; implement per-user Windows installed-program identity in existing AirWorker installer only with owned HKCU ARP key, exact display version/publisher/location and a genuinely supported remove contract; update transaction changes registry only after successful CLI/tray and native Claude/Codex cache identity, failure/rollback restores previous ARP and version; uninstall preserves owner data and does not remove unrelated files, report incomplete owned-bin deletion honestly; isolated native registry fixture and upgrade/rollback/uninstall tests including duplicate/downgrade, no live registry mutation until approved release; factual Go/PE checks, independent judge, exact source SHA and receipt, N130 overall open until installed acceptance
