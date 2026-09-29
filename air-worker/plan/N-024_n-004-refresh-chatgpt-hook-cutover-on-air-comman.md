---
id: "N-024_n-004-refresh-chatgpt-hook-cutover-on-air-comman"
title: "N-004 refresh ChatGPT hook cutover on AIR Commander r3.1"
parent: "N-004 ChatGPT host hook transport gap"
trigger: "AIR Commander r3.1 process-output patch is now live; old N-013 candidate cbc14b9 would regress that fix, so hook middleware must be rebased on r3.1 before live cutover."
owner: "AC·DEV·AirWorker"
done_when: "A new AIR Commander candidate based on live r3.1 includes ChatGPT AirWorker attach/status/pending/finalize/private-approve middleware plus process-output retention; targeted and full bridge regressions pass; source is committed/pushed with rollback to live r3.1; N-013 is closed as superseded; installation/restart remains a separate explicit LPR yes gate."
status: "closed"
return_to: "N-004 ChatGPT host hook transport gap"
created_at: "2026-09-29T11:25:14.6713312Z"
updated_at: "2026-09-29T11:34:09.8213682Z"
receipts:
  - "docs/receipts/AIRWORKER_N004_R32_HOOK_CANDIDATE_READY_20260929.md"
---

# N-004 refresh ChatGPT hook cutover on AIR Commander r3.1

- Родитель нити: N-004 ChatGPT host hook transport gap
- Владелец: AC·DEV·AirWorker
- Готово когда: A new AIR Commander candidate based on live r3.1 includes ChatGPT AirWorker attach/status/pending/finalize/private-approve middleware plus process-output retention; targeted and full bridge regressions pass; source is committed/pushed with rollback to live r3.1; N-013 is closed as superseded; installation/restart remains a separate explicit LPR yes gate.
