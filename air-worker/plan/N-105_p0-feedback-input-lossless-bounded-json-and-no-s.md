---
id: "N-105_p0-feedback-input-lossless-bounded-json-and-no-s"
title: "P0-FEEDBACK-INPUT: lossless bounded JSON and no stray CLI arguments"
parent: "N-104_p0-self-native-use-machine-verifiable-use-of-new"
trigger: "09.10.2026 real candidate AirWorker b864278 feedback add (run AW-N104-REAL-REVIEW-20261009-1): Windows CLI -text with nested quotes silently truncated after word unknown, but rc=0; reviewer then auto-applied less useful procedure, SHA 3fe022dc. Need fail-closed, JSON input and end-to-end text equality."
owner: "gpt-airworker-release-20261009"
done_when: "Native feedback add supports one bounded JSON -data-file with canonical keys and duplicate/unknown/non-string/oversize rejection before any module mutation; when -data-file used disallow conflicting direct metadata flags. Without data-file reject any fs positional tail after flag parsing, including Windows quote-break, with exit 2 and no event. Verify exact input SHA and identical observed/metadata/FB candidate after native full review, and run isolated candidate with real gpt-5.6-luna reviewer from a complete JSON observation; next real process loads auto-skill SHA and explicitly performs a supported native feedback action with verified procedure_used/outcome. Preserve all old shared feedback CLI compatibility, reviewer no_candidate, exact PID/runtime receipts and prevent automatic control approvals. Full Go/vet and independent Astra high, source commit+push, release/install separate LPR gates."
status: "open"
return_to: "N-104_p0-self-native-use-machine-verifiable-use-of-new"
created_at: "2026-10-09T04:49:26.5023964Z"
updated_at: "2026-10-09T04:49:26.5023964Z"
receipts:
---

# P0-FEEDBACK-INPUT: lossless bounded JSON and no stray CLI arguments

- Родитель нити: N-104_p0-self-native-use-machine-verifiable-use-of-new
- Владелец: gpt-airworker-release-20261009
- Готово когда: Native feedback add supports one bounded JSON -data-file with canonical keys and duplicate/unknown/non-string/oversize rejection before any module mutation; when -data-file used disallow conflicting direct metadata flags. Without data-file reject any fs positional tail after flag parsing, including Windows quote-break, with exit 2 and no event. Verify exact input SHA and identical observed/metadata/FB candidate after native full review, and run isolated candidate with real gpt-5.6-luna reviewer from a complete JSON observation; next real process loads auto-skill SHA and explicitly performs a supported native feedback action with verified procedure_used/outcome. Preserve all old shared feedback CLI compatibility, reviewer no_candidate, exact PID/runtime receipts and prevent automatic control approvals. Full Go/vet and independent Astra high, source commit+push, release/install separate LPR gates.
