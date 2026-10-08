---
id: "N-088_p1-text-nested-failed-and-nonzero-code-reject-su"
title: "P1-TEXT: nested failed and nonzero code reject success"
parent: "N-084_p1-mcp-nested-diagnostics-cannot-produce-false-p"
trigger: "Independent GPT-6 Astra high verdict CHANGES_REQUIRED on clean c00c167 (2026-10-08); LPR instructed continue under PLAN."
owner: "gpt-airworker-release-20261008"
done_when: "Astra c00c167 finding3: JSON MCP envelope with exit_code=0 and nested text failed, fail, error or process completed with exit code 1 MUST NOT create procedure_used or outcome pass. Extend observableTextOutcome including nonzero completion parsing with boundary-safe numeric match. Negative correlated node/PID diagnostic E2E must show zero usage events, positive diagnostics still pass. Full Go regression/vet, independent Astra high PASS on exact immutable commit."
status: "open"
return_to: "N-084_p1-mcp-nested-diagnostics-cannot-produce-false-p"
created_at: "2026-10-08T13:29:35.6463743Z"
updated_at: "2026-10-08T13:29:35.6463743Z"
receipts:
---

# P1-TEXT: nested failed and nonzero code reject success

- Родитель нити: N-084_p1-mcp-nested-diagnostics-cannot-produce-false-p
- Владелец: gpt-airworker-release-20261008
- Готово когда: Astra c00c167 finding3: JSON MCP envelope with exit_code=0 and nested text failed, fail, error or process completed with exit code 1 MUST NOT create procedure_used or outcome pass. Extend observableTextOutcome including nonzero completion parsing with boundary-safe numeric match. Negative correlated node/PID diagnostic E2E must show zero usage events, positive diagnostics still pass. Full Go regression/vet, independent Astra high PASS on exact immutable commit.
