# AirWorker 0.11.2 N-010 learned skills acceptance — 2026-09-29

Scope: Hermes-style indexed learned rules with body-on-demand.

Evidence:
- every verified active typed rule appears in a compact index with proposal id, class and description capped at 60 runes;
- `learn context -id` / `-class` loads full body only on demand;
- full body contains When to apply, Procedure, Pitfalls, Verification, including type/spec/test/receipt;
- legacy ledger-verified rules remain discoverable/readable;
- targeted tests `TestLearnSkill*`, `TestLearningContextLoadsOnlyLedgerVerifiedRules`, `TestLearnContext*` PASS in 1.539s.
