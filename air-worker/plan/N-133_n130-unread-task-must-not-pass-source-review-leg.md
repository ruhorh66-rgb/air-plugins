---
id: "N-133_n130-unread-task-must-not-pass-source-review-leg"
title: "N130 unread task must not pass source review (Legal19 HR14 regression)"
parent: "N-130_rc4-release-p0-source-reviewer-fixes-and-transac"
trigger: "LPR 2026-11-06: Legal19 and HR14 installed rc2 outer receipt success despite executor unable to read task; source review must fail closed without sandbox or permission bypass"
owner: "Dev Work 28 / SRVLM01"
done_when: "On exact frozen source SHA, native review source fails NOT_PROVEN/nonzero on unread task even if outer process reports ok=true, requires verified TASK_READ_OK, exact task SHA and real source citations before PASS; Legal19 and HR14 fixtures as regression, preserving successful safe read-only review and existing sandbox; scoped Go RC0, independent Anthropic reviewer PASS, immutable Git receipt and native node close only after proof. No foreign product edits."
status: "open"
return_to: "N-130_rc4-release-p0-source-reviewer-fixes-and-transac"
created_at: "2026-11-06T02:22:08.1225869Z"
updated_at: "2026-11-06T02:22:08.1225869Z"
receipts:
---

# N130 unread task must not pass source review (Legal19 HR14 regression)

- Родитель нити: N-130_rc4-release-p0-source-reviewer-fixes-and-transac
- Владелец: Dev Work 28 / SRVLM01
- Готово когда: On exact frozen source SHA, native review source fails NOT_PROVEN/nonzero on unread task even if outer process reports ok=true, requires verified TASK_READ_OK, exact task SHA and real source citations before PASS; Legal19 and HR14 fixtures as regression, preserving successful safe read-only review and existing sandbox; scoped Go RC0, independent Anthropic reviewer PASS, immutable Git receipt and native node close only after proof. No foreign product edits.
