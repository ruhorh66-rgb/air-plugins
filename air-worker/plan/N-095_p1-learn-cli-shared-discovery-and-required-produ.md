---
id: "N-095_p1-learn-cli-shared-discovery-and-required-produ"
title: "P1-LEARN-CLI: shared discovery and required product errors"
parent: "N-094_p1-learn-cli-feedback-shared-learning-cli-discov"
trigger: "ЛПР 08.10.2026: изучить сообщение эксплуатации GPT-5.6 Sol по N-094, включить в действующий PLAN AirWorker. Проверено по cmd/learn.go и cmd/learn_shared_cli.go: no-product shared actions попадают в legacy unknown action; root/action help расходится с доступными командами."
owner: "gpt-airworker-release-20261008"
done_when: "Implement single discoverable CLI dispatch/usage contract for shared and legacy learn actions. `air-worker learn --help` and `learn <action> --help` for all advertised shared actions exit 0 and describe actions accurately; `air-worker learn status -json` and `learn paths -json` without product exit 2 with machine-friendly, explicit '-product is required', NEVER 'unknown learn action', without choosing a product implicitly. With -product and shared selector, status/paths/context continue to work; legacy actions and self subcommands preserve semantics; unknown commands are still explicitly rejected. Update root help/plugin functional instructions without silently authorizing mutations. Tests: CLI built executable, exact error code and stdout/stderr in both modes, unknown commands, no product, duplicate product, action flags; full go test ./... -count=1 -timeout=180s and vet, product plan-lint, fresh checkout, machine JSON receipt with immutable commit, independent Astra high PASS before close. Keep N-093 P0 use/effect and N-094 dual feedback separate; release/install separate LPR gates."
status: "open"
return_to: "N-094_p1-learn-cli-feedback-shared-learning-cli-discov"
created_at: "2026-10-08T15:36:00.777782Z"
updated_at: "2026-10-08T15:36:00.777782Z"
receipts:
---

# P1-LEARN-CLI: shared discovery and required product errors

- Родитель нити: N-094_p1-learn-cli-feedback-shared-learning-cli-discov
- Владелец: gpt-airworker-release-20261008
- Готово когда: Implement single discoverable CLI dispatch/usage contract for shared and legacy learn actions. `air-worker learn --help` and `learn <action> --help` for all advertised shared actions exit 0 and describe actions accurately; `air-worker learn status -json` and `learn paths -json` without product exit 2 with machine-friendly, explicit '-product is required', NEVER 'unknown learn action', without choosing a product implicitly. With -product and shared selector, status/paths/context continue to work; legacy actions and self subcommands preserve semantics; unknown commands are still explicitly rejected. Update root help/plugin functional instructions without silently authorizing mutations. Tests: CLI built executable, exact error code and stdout/stderr in both modes, unknown commands, no product, duplicate product, action flags; full go test ./... -count=1 -timeout=180s and vet, product plan-lint, fresh checkout, machine JSON receipt with immutable commit, independent Astra high PASS before close. Keep N-093 P0 use/effect and N-094 dual feedback separate; release/install separate LPR gates.
