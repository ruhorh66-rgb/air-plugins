---
id: "N-115_next-rel-nous-portal-sonnet-opus-5-5-semantic-ju"
title: "NEXT-REL: Nous Portal Sonnet/Opus 5.5 semantic judges via Hermes"
parent: "N-114_next-rel-portal-nous-portal-omniroute-study-and-l"
trigger: "LPR 10.10.2026: approve a bounded Hermes/Nous Portal semantic-judge integration for AirWorker, using Sonnet 5.5 on simple steps and Opus 5.5 on complex steps, medium reasoning; OpenAI executor ladder remains unchanged; no OmniRoute integration."
owner: "gpt-airworker-release-20261010"
done_when: "Implement and test a read-only, bounded Hermes chat judge path using exact Nous Portal model IDs anthropic/claude-sonnet-5.5 and anthropic/claude-opus-5.5 with reasoning medium. Route simple/default steps to Sonnet and complex steps to Opus. Use Hermes CLI via safe stdin/query-file one-shot, max-turns 1, run budget, no tools; receipts must state provider nous, exact model and medium, failures/missing auth/model fail closed, and no direct Anthropic fallback. Preserve OpenAI/Codex executor ladder and existing Hermes/HERMES_HOME config, no direct credential reads, release/install not included. Add deterministic routing/argv/failure/receipt tests, validate config, full Go tests and vet; run one synthetic read-only live AirWorker judge call through the Nous Portal subscription after tests; preserve all unrelated WIP."
status: "closed"
return_to: "N-114_next-rel-portal-nous-portal-omniroute-study-and-l"
created_at: "2026-10-10T14:10:05.7818716Z"
updated_at: "2026-10-10T15:57:48.5856673Z"
receipts:
  - "AW-NOUS-JUDGES-SONNET55-OPUS55-MEDIUM-20261010-1; live synthetic AirWorker semantic PASS; receipt=R:/-4-/hermes/cache/scratch/airworker-hermes-judge-smoke-20261010-01/.woody/jobs/legacy-default-1-semantic-reviewer.receipt.json; tests=go test ./... -count=1 PASS; vet=go vet ./... PASS; validate=PASS"
---

# NEXT-REL: Nous Portal Sonnet/Opus 5.5 semantic judges via Hermes

- Родитель нити: N-114_next-rel-portal-nous-portal-omniroute-study-and-l
- Владелец: gpt-airworker-release-20261010
- Готово когда: Implement and test a read-only, bounded Hermes chat judge path using exact Nous Portal model IDs anthropic/claude-sonnet-5.5 and anthropic/claude-opus-5.5 with reasoning medium. Route simple/default steps to Sonnet and complex steps to Opus. Use Hermes CLI via safe stdin/query-file one-shot, max-turns 1, run budget, no tools; receipts must state provider nous, exact model and medium, failures/missing auth/model fail closed, and no direct Anthropic fallback. Preserve OpenAI/Codex executor ladder and existing Hermes/HERMES_HOME config, no direct credential reads, release/install not included. Add deterministic routing/argv/failure/receipt tests, validate config, full Go tests and vet; run one synthetic read-only live AirWorker judge call through the Nous Portal subscription after tests; preserve all unrelated WIP.
