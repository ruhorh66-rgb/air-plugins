---
id: "N-062_learn-p0-fix3-serialize-bootstrap-ownership-by-p"
title: "LEARN-P0-FIX3: serialize bootstrap ownership by product and runtime"
parent: "N-061_learn-p0-fix2-crash-safe-serialized-shared-learn"
trigger: "Independent gpt-6-astra high review of exact package 158128d62e9a7a880e41c11d14905ea6d064651c returned CHANGES_REQUIRED: same runtime root across distinct product roots is not covered by product-scoped bootstrap lock and can overwrite bootstrap intent before module ownership rejects the loser."
owner: "gpt-airworker-handoff-20261007"
done_when: "Bootstrap cutover acquires deterministic exclusive ownership covering both product root and runtime root before reading or publishing bootstrap intent/selector. Two distinct product roots targeting the same runtime cannot overwrite or replace each other bootstrap.json, selector, owner binding, or runtime identity; losing initializer fails without mutation. Add regression distinct products + shared runtime with delayed loser after winner selector publish. Targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged."
status: "closed"
return_to: "N-061_learn-p0-fix2-crash-safe-serialized-shared-learn"
created_at: "2026-10-07T07:02:26.2082256Z"
updated_at: "2026-10-07T10:51:39.8938312Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.7_SELF_LEARNING_CANDIDATE_20261007.json"
---

# LEARN-P0-FIX3: serialize bootstrap ownership by product and runtime

- Родитель нити: N-061_learn-p0-fix2-crash-safe-serialized-shared-learn
- Владелец: gpt-airworker-handoff-20261007
- Готово когда: Bootstrap cutover acquires deterministic exclusive ownership covering both product root and runtime root before reading or publishing bootstrap intent/selector. Two distinct product roots targeting the same runtime cannot overwrite or replace each other bootstrap.json, selector, owner binding, or runtime identity; losing initializer fails without mutation. Add regression distinct products + shared runtime with delayed loser after winner selector publish. Targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged.
