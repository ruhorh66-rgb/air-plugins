---
id: "N-013_n-004-controlled-live-hook-cutover-gate"
title: "N-004 controlled live hook cutover gate"
parent: "N-004 ChatGPT host hook transport gap"
trigger: "AIRCOMMANDER_GPT_AIRWORKER_CUTOVER_PACKAGE_20260929: LIVE CUTOVER GATED; source and preflight PASS."
owner: "AC·DEV·AirWorker"
done_when: "After explicit LPR live-cutover approval, startup.user.rules main working_directory is atomically switched to clean candidate cbc14b9, ASW 0.7.1 performs air-commander restart, new PID/lock/port/health are consistent, ChatGPT Refresh/new-session exposes AirWorker tools, live attach/pre/post/stop and private approval path PASS; rollback restores prior overlay/source if any check fails."
status: "closed"
return_to: "N-004 live transport acceptance"
created_at: "2026-09-29T04:02:27.9982271Z"
updated_at: "2026-09-29T11:34:09.8533669Z"
receipts:
  - "docs/receipts/AIRWORKER_N004_R32_HOOK_CANDIDATE_READY_20260929.md"
---

# N-004 controlled live hook cutover gate

- Родитель нити: N-004 ChatGPT host hook transport gap
- Владелец: AC·DEV·AirWorker
- Готово когда: After explicit LPR live-cutover approval, startup.user.rules main working_directory is atomically switched to clean candidate cbc14b9, ASW 0.7.1 performs air-commander restart, new PID/lock/port/health are consistent, ChatGPT Refresh/new-session exposes AirWorker tools, live attach/pre/post/stop and private approval path PASS; rollback restores prior overlay/source if any check fails.
