---
id: "N-061_learn-p0-fix2-crash-safe-serialized-shared-learn"
title: "LEARN-P0-FIX2: crash-safe serialized shared-learning bootstrap"
parent: "N-060_learn-p0-fix-bounded-reviewer-deadline-and-desce"
trigger: "Independent gpt-6-astra high review of exact 0.11.7 package 62b7a250baea320015ed4af10fbca436e6140e3b on 07.10.2026 returned CHANGES_REQUIRED with three P1 findings in learn_shared_bootstrap.go: non-resumable interrupted migration, unsafe compensation after post-target failure, and missing bootstrap serialization."
owner: "gpt-airworker-handoff-20261007"
done_when: "Shared-learning init is serialized per product; migration intent/progress is persisted before legacy rules move; restart resumes or safely rolls back incomplete activation; a module error after target write is reconciled before compensation and never leaves an untracked managed procedure or destroys recovery evidence; concurrent losing initializer cannot remove or replace winning selector/runtime. Add crash-after-selector, failure-after-target-write, and concurrent-init regressions. Targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged."
status: "closed"
return_to: "N-060_learn-p0-fix-bounded-reviewer-deadline-and-desce"
created_at: "2026-10-07T05:20:47.2212957Z"
updated_at: "2026-10-07T10:51:39.9260331Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.7_SELF_LEARNING_CANDIDATE_20261007.json"
---

# LEARN-P0-FIX2: crash-safe serialized shared-learning bootstrap

- Родитель нити: N-060_learn-p0-fix-bounded-reviewer-deadline-and-desce
- Владелец: gpt-airworker-handoff-20261007
- Готово когда: Shared-learning init is serialized per product; migration intent/progress is persisted before legacy rules move; restart resumes or safely rolls back incomplete activation; a module error after target write is reconciled before compensation and never leaves an untracked managed procedure or destroys recovery evidence; concurrent losing initializer cannot remove or replace winning selector/runtime. Add crash-after-selector, failure-after-target-write, and concurrent-init regressions. Targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged.
