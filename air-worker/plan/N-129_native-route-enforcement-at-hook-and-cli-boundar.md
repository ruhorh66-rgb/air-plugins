---
id: "N-129_native-route-enforcement-at-hook-and-cli-boundar"
title: "Native route enforcement at hook and CLI boundaries"
parent: "N-128_n128-native-claude-subscription-preflight-and-ho"
trigger: "ЛПР 2026-11-04: следующая LLM не должна запускать субагентов и судью неправильным путём; встроить проверяемые механические ограничения в hooks и ядро"
owner: "Dev Work 26 / SRVLM01"
done_when: "Installed plugin SessionStart injects native playbook before a session declaration; PreToolUse for active AirWorker sessions denies direct Codex exec/app-server and direct Claude model run, including supported AIR Commander process tools, without blocking safe diagnostics; product-scoped core refuses executor dispatch masquerading as two native subagents and requires independent Claude reviewer; targeted negative/positive tests and machine JSON receipts, independent Anthropic source verdict and Git push; frozen package/rollback candidate prepared, installed rc2 unchanged pending distinct LPR yes"
status: "open"
return_to: "N-031_learn-2-события-ядра-и-автомат"
created_at: "2026-11-04T09:37:36.7210041Z"
updated_at: "2026-11-04T09:37:36.7210041Z"
receipts:
---

# Native route enforcement at hook and CLI boundaries

- Родитель нити: N-128_n128-native-claude-subscription-preflight-and-ho
- Владелец: Dev Work 26 / SRVLM01
- Готово когда: Installed plugin SessionStart injects native playbook before a session declaration; PreToolUse for active AirWorker sessions denies direct Codex exec/app-server and direct Claude model run, including supported AIR Commander process tools, without blocking safe diagnostics; product-scoped core refuses executor dispatch masquerading as two native subagents and requires independent Claude reviewer; targeted negative/positive tests and machine JSON receipts, independent Anthropic source verdict and Git push; frozen package/rollback candidate prepared, installed rc2 unchanged pending distinct LPR yes
