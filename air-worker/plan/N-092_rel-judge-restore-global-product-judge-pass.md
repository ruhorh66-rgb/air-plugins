---
id: "N-092_rel-judge-restore-global-product-judge-pass"
title: "REL-JUDGE: restore global product judge PASS"
parent: "N-071_rel-0-11-9-next-airworker-release-with-shared-le"
trigger: "08.10.2026 scoped N-078 independent GPT-6 Astra high PASS on bc33045; separate installed AirWorker 0.11.8 product-wide judge -json returned rc=1 on exact same HEAD, 2 failing checks: check-ladder-reachable (Headroom transport package binary) and check-goal-drift (machine goal-verdict absent at first run). Keep N-071 release gate OPEN."
owner: "gpt-airworker-release-20261008"
done_when: "Investigate and resolve BOTH product-wide judge failures without weakening checks, changing min_facts, bypassing machine contract or editing learning journals: check-ladder-reachable must validate Headroom transport with real binary/receipt; check-goal-drift must validate current-HEAD machine goal verdict and goal distance after first publish. Reconcile installed 0.11.8 vs candidate 0.11.9 judge behavior. Perform bounded diagnostics and independent relevant tests. Require air-worker judge -product <canonical root> -json exit 0, exact head/goal fingerprint, 13 check results PASS, full Go test/vet, scoped N-078 accepted evidence preserved, JSON receipt and commit. The product release/tag/install/marketplace gates require separate explicit LPR yes; live 0.11.8 remains unchanged."
status: "open"
return_to: "N-071_rel-0-11-9-next-airworker-release-with-shared-le"
created_at: "2026-10-08T15:03:57.9914835Z"
updated_at: "2026-10-08T15:03:57.9914835Z"
receipts:
---

# REL-JUDGE: restore global product judge PASS

- Родитель нити: N-071_rel-0-11-9-next-airworker-release-with-shared-le
- Владелец: gpt-airworker-release-20261008
- Готово когда: Investigate and resolve BOTH product-wide judge failures without weakening checks, changing min_facts, bypassing machine contract or editing learning journals: check-ladder-reachable must validate Headroom transport with real binary/receipt; check-goal-drift must validate current-HEAD machine goal verdict and goal distance after first publish. Reconcile installed 0.11.8 vs candidate 0.11.9 judge behavior. Perform bounded diagnostics and independent relevant tests. Require air-worker judge -product <canonical root> -json exit 0, exact head/goal fingerprint, 13 check results PASS, full Go test/vet, scoped N-078 accepted evidence preserved, JSON receipt and commit. The product release/tag/install/marketplace gates require separate explicit LPR yes; live 0.11.8 remains unchanged.
