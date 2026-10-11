---
id: "N-124_p1-detect-cross-branch-node-id-collision-and-sta"
title: "P1 detect cross-branch node ID collision and stale authority before merge"
parent: "N-110_p0-auto-capture-persist-every-real-airworker-ses"
trigger: "N118 closed only in isolated branch, requiring N119. During this retrospective, native plan node new on a copy from d5ee3c4 reused N120 already assigned in sibling WIP branch 963b0bc; a second branch based on native N120 commit 06283c8 was required. AirWorker Wiki pointer had been stale. Curriculum T07/T08."
owner: "gpt-airworker-retro-curriculum-20261011"
done_when: "Before plan node allocation/close and status report, prove unique N ID across GitHub authoritative and active WIP branches or allocate globally collision-resistant ID; fail closed and return machine conflict/receipt without writing ambiguous PLAN/LEARN. Test simultaneous branches where one creates N120 and another sees older mainline, then verify deterministic N121 allocation with no manual plan edits. Also verify canonical PLAN/HEAD/dirty, source tree SHA, installed binary capability, product vault pointer and GitHub readback; preserve foreign dirty WIP, no force push. Codex Sol low independent PASS."
status: "open"
return_to: "N-110_p0-auto-capture-persist-every-real-airworker-ses"
created_at: "2026-10-11T05:52:46.4687172Z"
updated_at: "2026-10-11T05:52:46.4687172Z"
receipts:
---

# P1 detect cross-branch node ID collision and stale authority before merge

- Родитель нити: N-110_p0-auto-capture-persist-every-real-airworker-ses
- Владелец: gpt-airworker-retro-curriculum-20261011
- Готово когда: Before plan node allocation/close and status report, prove unique N ID across GitHub authoritative and active WIP branches or allocate globally collision-resistant ID; fail closed and return machine conflict/receipt without writing ambiguous PLAN/LEARN. Test simultaneous branches where one creates N120 and another sees older mainline, then verify deterministic N121 allocation with no manual plan edits. Also verify canonical PLAN/HEAD/dirty, source tree SHA, installed binary capability, product vault pointer and GitHub readback; preserve foreign dirty WIP, no force push. Codex Sol low independent PASS.
