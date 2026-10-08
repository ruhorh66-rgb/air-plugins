---
id: "N-075_recovery-module-restore-shared-learning-authorit"
title: "RECOVERY-MODULE: restore shared learning authority from GitHub without overwriting recovered WIP"
parent: "N-072_recovery-0-11-9-restore-one-authoritative-develo"
trigger: "LPR 08.10.2026 approved recovery; local air-modules checkout has HEAD 7bdbce9 but restored working files from an older snapshot, while GitHub dev/airworker-learning-20261005 is four commits ahead and contains the exact-number acceptance plus non-Go consumer acceptance."
owner: "gpt-airworker-release-20261008"
done_when: "Preserve the damaged local checkout and its patches; fetch/clone a new clean recovery worktree from GitHub branch dev/airworker-learning-20261005; verify it contains 7bdbce9 acceptance evidence and the later remote commits through current branch head; verify full tests/vet and relevant receipts on the clean source before making it the active module worktree for N-070. No reset/clean over the damaged checkout, no tag/release/install in this recovery node."
status: "open"
return_to: "N-070_handoff-after-0-11-8-publish-shared-learning-0-1"
created_at: "2026-10-08T03:41:01.8976996Z"
updated_at: "2026-10-08T03:41:01.8976996Z"
receipts:
---

# RECOVERY-MODULE: restore shared learning authority from GitHub without overwriting recovered WIP

- Родитель нити: N-072_recovery-0-11-9-restore-one-authoritative-develo
- Владелец: gpt-airworker-release-20261008
- Готово когда: Preserve the damaged local checkout and its patches; fetch/clone a new clean recovery worktree from GitHub branch dev/airworker-learning-20261005; verify it contains 7bdbce9 acceptance evidence and the later remote commits through current branch head; verify full tests/vet and relevant receipts on the clean source before making it the active module worktree for N-070. No reset/clean over the damaged checkout, no tag/release/install in this recovery node.
