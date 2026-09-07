# AirCoder — Ruflo 35-component assimilation matrix

Date: 2026-09-07
Upstream: `ruvnet/ruflo` current 35-plugin catalog; engine baseline `3.38.21`, rollback `3.38.11`.
Policy: every upstream plugin is assessed; integration may be runtime adoption, wrapper/contract, shared-service consumption, or pattern import. No silent omission.

| Ruflo plugin | AirCoder assimilation | First use |
|---|---|---|
| ruflo-core | ADOPT foundation surface | runtime health, plugin discovery, MCP/full-loop preflight |
| ruflo-swarm | ADOPT | substantial coding task decomposition and coordinated workers |
| ruflo-autopilot | ADOPT bounded | long coding loops with explicit acceptance/stop rules |
| ruflo-loop-workers | ADAPT | bounded background coding jobs; no second AIR scheduler |
| ruflo-workflows | ADOPT | reusable implementation/review/release workflows |
| ruflo-federation | ADAPT | future cross-node coding execution and authenticated delegation |
| ruflo-agentdb | CONSUME/ADOPT | agent execution memory and coding patterns; storage ownership remains shared |
| ruflo-rag-memory | CONSUME via shared memory/storage | retrieve prior code/decisions/solutions |
| ruflo-rvf | ADOPT | portable execution-memory checkpoints and rollbackable sessions |
| ruflo-ruvector | CONSUME/ADAPT | semantic code retrieval and graph/vector acceleration |
| ruflo-knowledge-graph | ADOPT | codebase entity/dependency knowledge graph |
| ruflo-intelligence | ADOPT | learn executor/task/model outcomes instead of custom learning DB |
| ruflo-graph-intelligence | ADOPT | impact/risk reasoning over code/dependency graphs |
| ruflo-daa | EXPERIMENT | adaptive agent behaviour under bounded coding scenarios |
| ruflo-ruvllm | INTEGRATE disabled-by-policy lane | local-model execution option without custom router duplication |
| ruflo-goals | ADOPT | goal decomposition, preconditions, replanning and acceptance tracking |
| ruflo-testgen | ADOPT | missing-test detection and regression generation |
| ruflo-browser | ADOPT | browser/UI acceptance for products that expose web surfaces |
| ruflo-jujutsu | ADOPT | diff risk, blast radius and reviewer selection |
| ruflo-docs | ADOPT | documentation generation and code/doc drift detection |
| ruflo-security-audit | ADOPT | dependency/CVE and release security gate |
| ruflo-aidefence | ADOPT | prompt-injection/PII/tool-input guard for coding agents |
| ruflo-adr | ADOPT | living ADR/change-decision record for AirCoder-driven development |
| ruflo-ddd | ADAPT | domain-boundary scaffold when task class warrants DDD |
| ruflo-sparc | ADAPT under AIR Vibe | implementation workflow; AIR standards remain outer contract |
| ruflo-metaharness | ADOPT | harness score/genome/MCP scan/threat model; evolution remains gated |
| ruflo-arena | ADOPT | A/B and tournament comparison of coding strategies/executors |
| ruflo-migrations | ADOPT | safe DB/schema migrations in coding work |
| ruflo-observability | ADOPT | spans/logs/metrics for executor runs and handoffs |
| ruflo-cost-tracker | ADOPT | token/model cost attribution and budget gates per verified result |
| ruflo-agent | ADOPT | sandboxed/local or managed agent execution surface |
| ruflo-plugin-creator | ADOPT | scaffold/validate/publish AIR plugins with drift checks |
| ruflo-iot-cognitum | PATTERN + domain lane | fleet/trust/anomaly patterns; activate for IoT coding tasks |
| ruflo-neural-trader | PATTERN + domain lane | multi-agent/backtest architecture patterns; activate for finance/trading code |
| ruflo-market-data | PATTERN + domain lane | ingestion/vectorization pipeline patterns for market-data products |

## Integration order

1. Full-loop foundation: core, swarm, workflows, goals, observability, cost-tracker, aidefence.
2. Coding quality: testgen, jujutsu, docs, security-audit, metaharness, arena.
3. Learning/memory: intelligence, AgentDB, RVF, RAG/knowledge graph/RuVector through shared storage contract.
4. Long-running/cross-node: autopilot, loop-workers, federation, agent.
5. Method/domain packs: ADR/DDD/SPARC/migrations/plugin-creator and all domain plugins.

## Non-negotiable handoff invariant

`AirCoder select -> machine preflight -> air-ruflo-bridge:run-via-ruflo -> canonical dry-run -> approved execution -> verification`.
No direct `claude -p`, no manual swarm/MCP orchestration, and no claim of Ruflo availability from plugin presence alone.