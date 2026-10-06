---
id: "N-048_rel-judge-astra-round2-selector-execution-and-no"
title: "REL-JUDGE: Astra round2 selector execution and node-source public fail-closed"
parent: "N-046"
trigger: "Independent gpt-6-astra high read-only review on exact HEAD 3ec583e627288d1722f23b0e9330d693dd9055aa, timeout 600s, exit 0, input_unchanged=true, returned CHANGES_REQUIRED with 2 P1 + 1 P2."
owner: "gpt-airworker-learning-20261005"
done_when: "Resolve all round2 findings: selected Go criterion execution must remove/override suppressing -list/-run/-skip/-short/count/bench/fuzz variants and fail UNKNOWN on unsupported -args; effective GOFLAGS cannot lend PASS or suppress selected execution; full-check PASS reuse is allowed only when unrestricted execution is proven. Add negative failing-selector tests for double-dash run, count=0, GOFLAGS, list, skip, short and test.* equivalents while retaining one inventory per check. Propagate canonical plan-node read failure into public factual judge UNKNOWN/nonzero and ordinary report nonzero, including no-criteria path; no legacy fallback. Run targeted + full regression, exact build/validate/judge, independent Astra 600s PASS. Receipt docs/receipts/LEARNING_20261006_ASTRA600_ROUND2_FIX.json."
status: "closed"
return_to: "N-046"
created_at: "2026-10-06T00:24:57.5065647Z"
updated_at: "2026-10-06T02:29:42.9362192Z"
receipts:
  - "docs/receipts/LEARNING_20261006_ASTRA600_ROUND2_FIX.json"
---

# REL-JUDGE: Astra round2 selector execution and node-source public fail-closed

- Родитель нити: N-046
- Владелец: gpt-airworker-learning-20261005
- Готово когда: Resolve all round2 findings: selected Go criterion execution must remove/override suppressing -list/-run/-skip/-short/count/bench/fuzz variants and fail UNKNOWN on unsupported -args; effective GOFLAGS cannot lend PASS or suppress selected execution; full-check PASS reuse is allowed only when unrestricted execution is proven. Add negative failing-selector tests for double-dash run, count=0, GOFLAGS, list, skip, short and test.* equivalents while retaining one inventory per check. Propagate canonical plan-node read failure into public factual judge UNKNOWN/nonzero and ordinary report nonzero, including no-criteria path; no legacy fallback. Run targeted + full regression, exact build/validate/judge, independent Astra 600s PASS. Receipt docs/receipts/LEARNING_20261006_ASTRA600_ROUND2_FIX.json.
