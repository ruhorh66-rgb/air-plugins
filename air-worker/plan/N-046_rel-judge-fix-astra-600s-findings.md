---
id: "N-046_rel-judge-fix-astra-600s-findings"
title: "REL-JUDGE: fix Astra 600s findings"
parent: "N-041"
trigger: "Independent gpt-6-astra high read-only review with 600s timeout on exact HEAD 01f043990a884604dbd63bb659e5f72f0fc7c720 returned CHANGES_REQUIRED, exit 0, input_unchanged=true."
owner: "gpt-airworker-learning-20261005"
done_when: "Fix all Astra 600s findings without weakening release proof: P1 unknown->known drift recovery must not dereference nil; P1 selector PASS reuse only when base Go check proves unrestricted execution; P1 K17/K18 must bind to real tamper enforcement tests; P1 K47 must have real external-result acceptance/no-second-model test or remain deferred; P2 reconcile K26/K24/K41/K46/K51/K54 with material tests or backlog; P2 canonical plan-node read errors fail UNKNOWN/NOT_PROVEN rather than fallback; P2 normalize go/go.exe command identity for selector inventory. Add negative regressions, full go test/vet, validate/goals/plan-lint, exact factual judge and independent Astra PASS. Receipt docs/receipts/LEARNING_20261006_ASTRA600_FIX.json."
status: "closed"
return_to: "N-041"
created_at: "2026-10-05T23:59:18.7889014Z"
updated_at: "2026-10-06T02:29:43.039672Z"
receipts:
  - "docs/receipts/LEARNING_20261006_ASTRA600_FIX.json"
---

# REL-JUDGE: fix Astra 600s findings

- Родитель нити: N-041
- Владелец: gpt-airworker-learning-20261005
- Готово когда: Fix all Astra 600s findings without weakening release proof: P1 unknown->known drift recovery must not dereference nil; P1 selector PASS reuse only when base Go check proves unrestricted execution; P1 K17/K18 must bind to real tamper enforcement tests; P1 K47 must have real external-result acceptance/no-second-model test or remain deferred; P2 reconcile K26/K24/K41/K46/K51/K54 with material tests or backlog; P2 canonical plan-node read errors fail UNKNOWN/NOT_PROVEN rather than fallback; P2 normalize go/go.exe command identity for selector inventory. Add negative regressions, full go test/vet, validate/goals/plan-lint, exact factual judge and independent Astra PASS. Receipt docs/receipts/LEARNING_20261006_ASTRA600_FIX.json.
