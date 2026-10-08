---
id: "N-081_p0-stop-durable-dual-owner-completion-and-deferr"
title: "P0-STOP: durable dual-owner completion and deferred review"
parent: "N-078_p0-self-owner-astra-remediate-seven-release-bloc"
trigger: "ЛПР 08.10.2026: с замечаниями по незакрытому блокирующему Stop согласен, внести в план и продолжать."
owner: "gpt-airworker-release-20261008"
done_when: "LPR approved 2026-10-08 finding #7 fix before Astra. Stop must durably persist run_completed for AirWorker self and target owners (dedup same root) before slow review, finish within 5s hook even if reviewer runs 240s or fails, use pinned learning defer_review then explicit bounded review with durable resume on crash/restart and no duplicate callbacks/proposals. Negative tests: slow reviewer, failure, duplicate Stop, first/second owner failure, restart. Proof: exact event IDs/run/principal/session, both journals, review eventually recorded, no spurious PASS. Judge: named Go tests, full go test ./... -count=1 -timeout=180s and go vet exit 0, dual-owner E2E, fresh core.autocrlf checkout, independent gpt-6-astra high PASS for exact immutable commit. Receipt JSON in docs/receipts. No tag/release/install/reboot/UAC/stop foreign services without separate explicit LPR yes."
status: "closed"
return_to: "N-078_p0-self-owner-astra-remediate-seven-release-bloc"
created_at: "2026-10-08T10:05:17.4186105Z"
updated_at: "2026-10-08T15:01:49.553787Z"
receipts:
  - "R:\\-4-\\air-worker\\work\\0.11.9\\n090\\N078_ACCEPTANCE_bc33045.json"
---

# P0-STOP: durable dual-owner completion and deferred review

- Родитель нити: N-078_p0-self-owner-astra-remediate-seven-release-bloc
- Владелец: gpt-airworker-release-20261008
- Готово когда: LPR approved 2026-10-08 finding #7 fix before Astra. Stop must durably persist run_completed for AirWorker self and target owners (dedup same root) before slow review, finish within 5s hook even if reviewer runs 240s or fails, use pinned learning defer_review then explicit bounded review with durable resume on crash/restart and no duplicate callbacks/proposals. Negative tests: slow reviewer, failure, duplicate Stop, first/second owner failure, restart. Proof: exact event IDs/run/principal/session, both journals, review eventually recorded, no spurious PASS. Judge: named Go tests, full go test ./... -count=1 -timeout=180s and go vet exit 0, dual-owner E2E, fresh core.autocrlf checkout, independent gpt-6-astra high PASS for exact immutable commit. Receipt JSON in docs/receipts. No tag/release/install/reboot/UAC/stop foreign services without separate explicit LPR yes.
