---
id: "N-063_learn-p0-fix4-canonical-runtime-identity-lock-on"
title: "LEARN-P0-FIX4: canonical runtime identity lock on Windows aliases"
parent: "N-062_learn-p0-fix3-serialize-bootstrap-ownership-by-p"
trigger: "Independent gpt-6-astra high review of exact package 847ac445d57d466fe109ccf5bb1909c8536f4cab returned CHANGES_REQUIRED: runtime-847ac44 and runtime-847ac44. resolve to the same Windows directory but produce different sibling lock paths, leaving a release-blocking alias race."
owner: "gpt-airworker-handoff-20261007"
done_when: "Windows bootstrap runtime ownership is keyed by canonical filesystem identity, not raw path spelling; unsupported Win32 aliases such as trailing dot/space are rejected before bootstrap I/O. Distinct product roots using aliased spellings of the same runtime cannot acquire separate locks or overwrite winner bootstrap/selector/owner/identity. Add deterministic alias regression, then targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged."
status: "closed"
return_to: "N-062_learn-p0-fix3-serialize-bootstrap-ownership-by-p"
created_at: "2026-10-07T07:21:26.1242126Z"
updated_at: "2026-10-07T10:51:39.8552849Z"
receipts:
  - "docs/receipts/AIRWORKER_0.11.7_SELF_LEARNING_CANDIDATE_20261007.json"
---

# LEARN-P0-FIX4: canonical runtime identity lock on Windows aliases

- Родитель нити: N-062_learn-p0-fix3-serialize-bootstrap-ownership-by-p
- Владелец: gpt-airworker-handoff-20261007
- Готово когда: Windows bootstrap runtime ownership is keyed by canonical filesystem identity, not raw path spelling; unsupported Win32 aliases such as trailing dot/space are rejected before bootstrap I/O. Distinct product roots using aliased spellings of the same runtime cannot acquire separate locks or overwrite winner bootstrap/selector/owner/identity. Add deterministic alias regression, then targeted tests, full regression, exact clean package E2E and independent gpt-6-astra high PASS. Publication/install gates unchanged.
