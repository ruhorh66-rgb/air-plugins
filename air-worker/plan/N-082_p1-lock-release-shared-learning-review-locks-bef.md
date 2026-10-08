---
id: "N-082_p1-lock-release-shared-learning-review-locks-bef"
title: "P1-LOCK: release shared-learning review locks before slow callbacks"
parent: "N-081_p0-stop-durable-dual-owner-completion-and-deferr"
trigger: "ЛПР 08.10.2026 после Astra high CHANGES_REQUIRED 4xP1: дорабатывай план, продолжай работу."
owner: "gpt-airworker-release-20261008"
done_when: "Fix Astra P1 review lock starvation at pinned module commit 4e63a520. Review/LLM callback must not hold GitRoot or RuntimeRoot locks; persist durable claim/reviewing state first, run bounded callbacks without mutation locks, then revalidate/generation-check and persist result transactionally; concurrent Stop for independent run and both owners remains <5s even with actively blocked reviewer. Keep idempotency of proposal/event; interrupted review resumes without double effect. Use isolated air-modules worktree/branch, preserve foreign dirty WIP. Judge: adversarial concurrent subprocess test slow reviewer+new Stop, crash/retry tests, module Go full test/vet, AirWorker full test/vet, fresh E2E, separate Astra high PASS, machine receipts; release/install separate gates."
status: "open"
return_to: "N-081_p0-stop-durable-dual-owner-completion-and-deferr"
created_at: "2026-10-08T11:53:52.9848728Z"
updated_at: "2026-10-08T11:53:52.9848728Z"
receipts:
---

# P1-LOCK: release shared-learning review locks before slow callbacks

- Родитель нити: N-081_p0-stop-durable-dual-owner-completion-and-deferr
- Владелец: gpt-airworker-release-20261008
- Готово когда: Fix Astra P1 review lock starvation at pinned module commit 4e63a520. Review/LLM callback must not hold GitRoot or RuntimeRoot locks; persist durable claim/reviewing state first, run bounded callbacks without mutation locks, then revalidate/generation-check and persist result transactionally; concurrent Stop for independent run and both owners remains <5s even with actively blocked reviewer. Keep idempotency of proposal/event; interrupted review resumes without double effect. Use isolated air-modules worktree/branch, preserve foreign dirty WIP. Judge: adversarial concurrent subprocess test slow reviewer+new Stop, crash/retry tests, module Go full test/vet, AirWorker full test/vet, fresh E2E, separate Astra high PASS, machine receipts; release/install separate gates.
