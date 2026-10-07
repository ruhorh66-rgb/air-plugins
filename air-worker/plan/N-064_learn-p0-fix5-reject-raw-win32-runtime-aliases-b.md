---
id: "N-064_learn-p0-fix5-reject-raw-win32-runtime-aliases-b"
title: "LEARN-P0-FIX5: reject raw Win32 runtime aliases before normalization"
parent: "N-063_learn-p0-fix4-canonical-runtime-identity-lock-on"
trigger: "Independent gpt-6-astra high review of exact package 9794d7956275df64fdd691326d81d5eba825194c returned CHANGES_REQUIRED P2: initSharedLearning trims raw runtime-root before validation, admitting trailing-space aliases, and validator omits the Windows NT device prefix \\\\??\\\\."
owner: "gpt-airworker-handoff-20261007"
done_when: "Windows init-shared validates the original raw runtime-root argument before TrimSpace/Clean or filesystem I/O; trailing-dot/trailing-space components and Win32/NT device prefixes including \\\\?\\\\, \\\\.\\\\ and \\\\??\\\\ are rejected fail-closed. Add regressions for trailing space and device-path aliases proving no bootstrap intent, selector, owner binding or runtime mutation occurs. Targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged."
status: "closed"
return_to: "N-063_learn-p0-fix4-canonical-runtime-identity-lock-on"
created_at: "2026-10-07T07:38:10.5377731Z"
updated_at: "2026-10-07T10:51:39.7716741Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.7_SELF_LEARNING_CANDIDATE_20261007.json"
---

# LEARN-P0-FIX5: reject raw Win32 runtime aliases before normalization

- Родитель нити: N-063_learn-p0-fix4-canonical-runtime-identity-lock-on
- Владелец: gpt-airworker-handoff-20261007
- Готово когда: Windows init-shared validates the original raw runtime-root argument before TrimSpace/Clean or filesystem I/O; trailing-dot/trailing-space components and Win32/NT device prefixes including \\?\\, \\.\\ and \\??\\ are rejected fail-closed. Add regressions for trailing space and device-path aliases proving no bootstrap intent, selector, owner binding or runtime mutation occurs. Targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged.
