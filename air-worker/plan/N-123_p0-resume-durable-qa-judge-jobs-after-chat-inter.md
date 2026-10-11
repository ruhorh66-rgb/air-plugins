---
id: "N-123_p0-resume-durable-qa-judge-jobs-after-chat-inter"
title: "P0 resume durable QA judge jobs after chat interruption without duplicates"
parent: "N-110_p0-auto-capture-persist-every-real-airworker-ses"
trigger: "Eleven QA_FULL receipts on distinct source SHA plus long Go processes and manual polling; stream breaks triggered repeated human queries and transient wrapper errors. Curriculum T06 and holdout H02."
owner: "gpt-airworker-retro-curriculum-20261011"
done_when: "For every Go/full QA/judge command create a durable job ID keyed by source SHA+operation+arguments, bounded deadline, single owning process tree, immutable JSON terminal status exit/log SHA and recovery_ref. Demonstrate two forced message disconnects and one EXECUTION_UNKNOWN: next session recovers same job and result with zero duplicate run and no blind rerun, no foreign process kill. Stage targeted/adversarial then one full QA on final clean SHA; independent Codex Sol low PASS and reversible rollback receipt."
status: "open"
return_to: "N-110_p0-auto-capture-persist-every-real-airworker-ses"
created_at: "2026-10-11T05:52:46.4285753Z"
updated_at: "2026-10-11T05:52:46.4285753Z"
receipts:
---

# P0 resume durable QA judge jobs after chat interruption without duplicates

- Родитель нити: N-110_p0-auto-capture-persist-every-real-airworker-ses
- Владелец: gpt-airworker-retro-curriculum-20261011
- Готово когда: For every Go/full QA/judge command create a durable job ID keyed by source SHA+operation+arguments, bounded deadline, single owning process tree, immutable JSON terminal status exit/log SHA and recovery_ref. Demonstrate two forced message disconnects and one EXECUTION_UNKNOWN: next session recovers same job and result with zero duplicate run and no blind rerun, no foreign process kill. Stage targeted/adversarial then one full QA on final clean SHA; independent Codex Sol low PASS and reversible rollback receipt.
