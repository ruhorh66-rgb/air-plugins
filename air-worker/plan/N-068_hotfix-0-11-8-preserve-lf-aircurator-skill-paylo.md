---
id: "N-068_hotfix-0-11-8-preserve-lf-aircurator-skill-paylo"
title: "HOTFIX-0.11.8: preserve LF AirCurator skill payloads in marketplace checkout"
parent: "N-067_live-0-11-7-promote-marketplace-install-refresh-c"
trigger: "Post-install selfcheck on 07.10.2026 found both canonical Claude/Codex 0.11.7 marketplace caches have CRLF-converted AirCurator SKILL.md payloads. Raw SHA mismatches registry, while CRLF-to-LF canonical SHA matches all three registry values exactly. Content is unchanged; checkout EOL policy is missing."
owner: "gpt-airworker-handoff-20261007"
done_when: "GitHub marketplace checkout on Windows with core.autocrlf enabled preserves release-owned skills/*/SKILL.md bytes as LF so raw registry SHA remains authoritative. Add scoped .gitattributes, bump canonical AirWorker version to 0.11.8, and prove an autocrlf=true clone/package health/selfcheck has exact registry hashes. Full Go/vet, check-plugin, clean-session, self-learning regression and independent gpt-6-astra high PASS. Prepare exact 0.11.8 publication/install gate; do not republish 0.11.7 or mutate caches manually. Current live 0.11.7 may remain installed during candidate preparation; rollback 0.11.6 remains available."
status: "open"
return_to: "N-067_live-0-11-7-promote-marketplace-install-refresh-c"
created_at: "2026-10-07T12:24:15.2949023Z"
updated_at: "2026-10-07T12:24:15.2949023Z"
receipts:
---

# HOTFIX-0.11.8: preserve LF AirCurator skill payloads in marketplace checkout

- Родитель нити: N-067_live-0-11-7-promote-marketplace-install-refresh-c
- Владелец: gpt-airworker-handoff-20261007
- Готово когда: GitHub marketplace checkout on Windows with core.autocrlf enabled preserves release-owned skills/*/SKILL.md bytes as LF so raw registry SHA remains authoritative. Add scoped .gitattributes, bump canonical AirWorker version to 0.11.8, and prove an autocrlf=true clone/package health/selfcheck has exact registry hashes. Full Go/vet, check-plugin, clean-session, self-learning regression and independent gpt-6-astra high PASS. Prepare exact 0.11.8 publication/install gate; do not republish 0.11.7 or mutate caches manually. Current live 0.11.7 may remain installed during candidate preparation; rollback 0.11.6 remains available.
