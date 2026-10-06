---
id: "N-054_rel-0-11-7-aircurator-skill-lifecycle-candidate-a"
title: "REL-0.11.7: AirCurator skill lifecycle candidate and exact publication/install gate"
parent: "N-053"
trigger: "LPR 06.10.2026: implement release-owned AirCurator skill lifecycle after verified 0.11.6; prepare evidence/exact commit and next release gate, but do not publish or install the next release without a separate exact LPR gate."
owner: "gpt-airworker-curator-skill-lifecycle-20261006"
done_when: "Prepare AirWorker 0.11.7 candidate containing only verified N-053 AirCurator skill lifecycle changes from base main c31e6acc3fc0e8b95bf8794ce39bf04db97fcc3d. Keep curator/report-every-patrol 5f4a443c306bc27b16c53c408aa00ea03cba203e, N-031 wiki/SQL/learning/grants and all DB copy/restore out of scope. Bump canonical version/changelog, build CLI+tray from clean checkout with vcs.revision equal exact source and vcs.modified=false, run full Go/vet, plan-lint/goals/validate, check-plugin package-health, Windows clean Claude/Codex sync/status/SessionStart smoke, negative missing-skill and mirror-drift tests, no-secret scan and independent gpt-6-astra high PASS. Record source/package SHA and rollback target 0.11.6. Publication/tag/push and installation/cutover are NOT authorized by this node; after candidate PASS wait for separate explicit LPR approval naming exact source/package SHA for publication and separate explicit yes for installation. Receipt docs/receipts/AIRWORKER_0.11.7_AIRCURATOR_SKILLS_CANDIDATE_20261006.json."
status: "open"
return_to: "N-053"
created_at: "2026-10-06T08:22:42.2250709Z"
updated_at: "2026-10-06T08:22:42.2250709Z"
receipts:
---

# REL-0.11.7: AirCurator skill lifecycle candidate and exact publication/install gate

- Родитель нити: N-053
- Владелец: gpt-airworker-curator-skill-lifecycle-20261006
- Готово когда: Prepare AirWorker 0.11.7 candidate containing only verified N-053 AirCurator skill lifecycle changes from base main c31e6acc3fc0e8b95bf8794ce39bf04db97fcc3d. Keep curator/report-every-patrol 5f4a443c306bc27b16c53c408aa00ea03cba203e, N-031 wiki/SQL/learning/grants and all DB copy/restore out of scope. Bump canonical version/changelog, build CLI+tray from clean checkout with vcs.revision equal exact source and vcs.modified=false, run full Go/vet, plan-lint/goals/validate, check-plugin package-health, Windows clean Claude/Codex sync/status/SessionStart smoke, negative missing-skill and mirror-drift tests, no-secret scan and independent gpt-6-astra high PASS. Record source/package SHA and rollback target 0.11.6. Publication/tag/push and installation/cutover are NOT authorized by this node; after candidate PASS wait for separate explicit LPR approval naming exact source/package SHA for publication and separate explicit yes for installation. Receipt docs/receipts/AIRWORKER_0.11.7_AIRCURATOR_SKILLS_CANDIDATE_20261006.json.
