# AirWorker 0.11.2 N-011 learn lifecycle acceptance — 2026-09-29

Scope: canonical `learn/events.jsonl` visibility for `learn apply` and `learn rollback`.

Evidence:
- `learnEventKinds` accepts `lifecycle`.
- successful apply appends a canonical lifecycle event referencing proposal and apply ledger.
- successful rollback appends a canonical lifecycle event referencing proposal, rollback ledger and target apply ledger.
- test `TestLearnRollbackRestoresExactPreviousBytes` requires exactly two lifecycle events for apply+rollback and validates evidence links.
- targeted regression PASS: `TestLearnApplyRequiresUserPromptApprovalGrant`, `TestLearnRollbackRestoresExactPreviousBytes`, `TestLearnRollbackRefusesToClobberNewerRules`, generic executable apply transactions.
- targeted `go test` PASS in 2.623s; `git diff --check` PASS.
