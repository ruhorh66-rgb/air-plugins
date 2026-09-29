---
id: "N-016_0-11-3-anthropic-judges-through-headroom"
title: "0.11.3 Anthropic judges through Headroom"
parent: "N-014 vendor model ladder"
trigger: "LPR requires Headroom transport for Anthropic calls."
owner: "AC·DEV·AirWorker"
done_when: "Every AirWorker Claude semantic-judge/planner invocation routes through ANTHROPIC_BASE_URL=http://localhost:8787, preserves existing user OAuth credential handling, preflight verifies Headroom health plus one real Claude smoke, and direct bypass is rejected by regression tests."
status: "open"
return_to: "N-014 vendor model ladder"
created_at: "2026-09-29T06:04:09.1315674Z"
updated_at: "2026-09-29T06:04:09.1315674Z"
receipts:
---

# 0.11.3 Anthropic judges through Headroom

- Родитель нити: N-014 vendor model ladder
- Владелец: AC·DEV·AirWorker
- Готово когда: Every AirWorker Claude semantic-judge/planner invocation routes through ANTHROPIC_BASE_URL=http://localhost:8787, preserves existing user OAuth credential handling, preflight verifies Headroom health plus one real Claude smoke, and direct bypass is rejected by regression tests.
