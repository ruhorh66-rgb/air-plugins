---
id: "N-021_0-11-3-codex-multiline-prompt-transport-via-stdi"
title: "0.11.3 Codex multiline prompt transport via stdin"
parent: "N-020 Ponytail noninteractive task-consumption fix"
trigger: "N-018 real smoke proved multiline prompt tail is lost when passed through Windows codex.cmd argv."
owner: "AC·DEV·AirWorker"
done_when: "Every AirWorker Codex executor, orchestration subagent/leader, planner and LEARN review sends the full Ponytail+task prompt through stdin using codex exec -; argv contains no multiline task payload; unit tests prove stdin carries both vendor skill and task; N-018 real coding smoke edits only the disposable target."
status: "open"
return_to: "N-020 Ponytail noninteractive task-consumption fix"
created_at: "2026-09-29T08:03:35.2390458Z"
updated_at: "2026-09-29T08:03:35.2390458Z"
receipts:
---

# 0.11.3 Codex multiline prompt transport via stdin

- Родитель нити: N-020 Ponytail noninteractive task-consumption fix
- Владелец: AC·DEV·AirWorker
- Готово когда: Every AirWorker Codex executor, orchestration subagent/leader, planner and LEARN review sends the full Ponytail+task prompt through stdin using codex exec -; argv contains no multiline task payload; unit tests prove stdin carries both vendor skill and task; N-018 real coding smoke edits only the disposable target.
