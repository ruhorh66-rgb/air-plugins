---
id: "N-113_p1-canonicalize-windows-profile-aliases-in-distr"
title: "P1: canonicalize Windows profile aliases in distribution selfcheck"
parent: "N-071_rel-0-11-9-next-airworker-release-with-shared-le"
trigger: "LPR 10.10.2026: fix the installed AirWorker selfcheck failure caused by canonical Codex profile aliases and the unneeded curator role for this task"
owner: "gpt-airworker-release-20261010"
done_when: "Fix the 0.11.8 Windows selfcheck failure when CODEX_HOME points to R:/-4-/codex-home while USERPROFILE/.codex is a junction to the same physical directory. Canonicalize/deduplicate config roots by resolved filesystem identity before ambiguity and package-health checks; inspect each physical package root once using a stable canonical path. Preserve warnings/failures for genuinely distinct profiles and missing/invalid release payloads. Add Windows tests for junction aliases, identical roots, separate profiles, active-profile selection, and payload resolution through aliased paths. Verify default selfcheck and install-status on SRVLM01 pass with normal environment and canonical Claude/Codex profiles, without process-scoped USERPROFILE workaround or manual cache edits; record exact source/package/CLI hashes. Keep code mutation, release, and install behind the existing project gates. AirCurator package remains governed by its existing release contract; do not remove or invoke curator functionality for this fix."
status: "open"
return_to: "N-071_rel-0-11-9-next-airworker-release-with-shared-le"
created_at: "2026-10-10T13:36:07.6287846Z"
updated_at: "2026-10-10T13:36:07.6287846Z"
receipts:
---

# P1: canonicalize Windows profile aliases in distribution selfcheck

- Родитель нити: N-071_rel-0-11-9-next-airworker-release-with-shared-le
- Владелец: gpt-airworker-release-20261010
- Готово когда: Fix the 0.11.8 Windows selfcheck failure when CODEX_HOME points to R:/-4-/codex-home while USERPROFILE/.codex is a junction to the same physical directory. Canonicalize/deduplicate config roots by resolved filesystem identity before ambiguity and package-health checks; inspect each physical package root once using a stable canonical path. Preserve warnings/failures for genuinely distinct profiles and missing/invalid release payloads. Add Windows tests for junction aliases, identical roots, separate profiles, active-profile selection, and payload resolution through aliased paths. Verify default selfcheck and install-status on SRVLM01 pass with normal environment and canonical Claude/Codex profiles, without process-scoped USERPROFILE workaround or manual cache edits; record exact source/package/CLI hashes. Keep code mutation, release, and install behind the existing project gates. AirCurator package remains governed by its existing release contract; do not remove or invoke curator functionality for this fix.
