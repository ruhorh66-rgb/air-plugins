# AirWorker 0.11.3 — vendor model ladder proposal

Status: **PROPOSAL ONLY / AWAITING EXPLICIT LPR APPROVAL**  
Node: `N-014_0-11-3-vendor-model-ladder-openai-executors-and-a`  
Date: 2026-09-29  
Node: SRVLM01

No production ladder/config/runtime was changed by this proposal.

## 1. Verified local tool facts

### OpenAI / Codex

AirWorker resolves Codex from:

- executable: `R:\-4-\npm-global\codex.cmd`
- PATH CLI version observed: `codex-cli 0.145.0`
- `CODEX_HOME=R:\-4-\codex-home`

Fresh local Codex picker/model catalog:

- file: `R:\-4-\codex-home\models_cache.json`
- fetched_at: `2026-09-29T05:01:18.486860400Z`
- catalog client_version: `0.158.0`

All requested OpenAI IDs are present with `visibility=list` and `supported_in_api=true`:

| ID | local reasoning efforts |
|---|---|
| `gpt-5.5` | low, medium, high, xhigh |
| `gpt-5.6-luna` | low, medium, high, xhigh, max |
| `gpt-5.6-terra` | low, medium, high, xhigh, max, ultra |
| `gpt-5.6-sol` | low, medium, high, xhigh, max, ultra |
| `gpt-6-luna` | low, medium, high, xhigh, max |
| `gpt-6-sol` | low, medium, high, xhigh, max, ultra |
| `gpt-6-astra` | low, medium, high, xhigh, max, ultra |

The direct one-line Codex execution probe was blocked by the current command-policy layer before a valid terminal model event could be established. Therefore this proposal uses the fresh local CLI picker catalog as the local availability fact; a real one-line `codex` smoke for each primary model is a mandatory pre-release gate after LPR approval.

### Anthropic / Claude Code

AirWorker resolves Claude from:

- executable: `C:\Users\admin_loc\.local\bin\claude.exe`
- CLI version observed: `2.1.274`
- `CLAUDE_CONFIG_DIR=R:\-4-\claude-home`

The judge is **not** authorized through interactive `claude auth status`. AirWorker's existing `runnerEnv()` injects the user-level `CLAUDE_CODE_OAUTH_TOKEN` when it is absent from the parent process. On SRVLM01 the user-level credential presence check returned true; its value was never printed.

Valid preflight path: credential presence check + normal Claude Code trial call.  
Invalid preflight path: `claude auth status` alone.  
Interactive account login remains LPR-only.

Real normal-mode trial:

- model: `claude-sonnet-5-5`
- result: `RC=0`
- terminal_reason: `completed`
- canonicalModel: `claude-sonnet-5-5`
- is_error: false

Claude CLI 2.1.274 emitted a local `unrecognized_model` warning for Sonnet 5.5 while the first-party backend accepted and completed the model successfully. Treat the warning as compatibility telemetry, not an auth failure. A test with `--bare` is rejected as evidence because bare mode intentionally disables the normal OAuth/keychain path and returned `api_error` for all candidates.

## 2. Exact Anthropic IDs to register

Allowed catalog, with LPR floor starting at Sonnet 4.6 for judge policy:

- `claude-sonnet-4-6`
- `claude-opus-4-6`
- `claude-opus-4-7`
- `claude-opus-4-8`
- `claude-fable-5`
- `claude-opus-5`
- `claude-sonnet-5`
- `claude-haiku-4-5` (alias; canonical pre-4.6 snapshot is dated; catalog only, excluded by Sonnet-4.6 judge floor)
- `claude-sonnet-5-5`
- `claude-fable-5-1`
- `claude-opus-5-5`

For 4.6+ Anthropic uses dateless canonical pinned IDs. Pre-4.6 models use dated canonical snapshots with aliases.

## 3. Price/quality finding

A purely generation-ordered linear ladder is not cost-efficient.

OpenAI standard prices currently make the GPT-6 primary route Pareto-better than several older models:

| Model | input / MTok | output / MTok | role |
|---|---:|---:|---|
| `gpt-6-luna` | $0.10 | $0.50 | cheapest primary |
| `gpt-5.6-luna` | $0.20 | $1.20 | fallback only |
| `gpt-6-sol` | $2.00 | $10.00 | default primary |
| `gpt-5.6-terra` | $2.00 | $12.00 | fallback only |
| `gpt-5.6-sol` | $4.00 | $20.00 | fallback only |
| `gpt-5.5` | $5.00 | $30.00 | compatibility/quality fallback |
| `gpt-6-astra` | $10.00 | $50.00 | hardest work |

Anthropic latest judge prices:

| Model | input / MTok | output / MTok | proposed role |
|---|---:|---:|---|
| `claude-sonnet-5-5` | $2 | $10 | default semantic judge |
| `claude-opus-5-5` | $4 | $20 | complex judge |
| `claude-fable-5-1` | $10 | $50 | exceptional arbitration only |

`claude-sonnet-4-6` is currently legacy and costs $3/$15, so it is a valid LPR floor/fallback but should not precede Sonnet 5.5 in the normal path. Older Opus 4.6/4.7/4.8 and Opus 5 are likewise registered as availability fallbacks, not normal escalation.

## 4. Proposed executor ladder — request for LPR approval

### Primary OpenAI path

1. `script`
2. `gpt-6-luna:medium` — bulk/focused work
3. `gpt-6-luna:max` — cheap retry before changing model family
4. `gpt-6-sol:medium` — default coding/agentic work
5. `gpt-6-sol:high` — hard coding/reasoning
6. `gpt-6-astra:high` — hardest end-to-end work
7. `gpt-6-astra:max` — terminal executor tier before human/LPR escalation

No Anthropic model appears in the executor primary path.

### Registered OpenAI fallback pool

All are registered and selectable by the core, but are used only on `vendor_limit`, model-unavailable, or explicit LPR/operator override:

- `gpt-5.6-luna`
- `gpt-5.6-terra`
- `gpt-5.6-sol`
- `gpt-5.5`

Proposed mapping:

- primary `gpt-6-luna` -> fallback `gpt-5.6-luna`
- primary `gpt-6-sol` -> fallbacks `gpt-5.6-terra`, then `gpt-5.6-sol`, then `gpt-5.5`
- primary `gpt-6-astra` -> fallbacks `gpt-5.6-sol`, then `gpt-5.5`

This keeps every allowed OpenAI model in the routing registry without paying more for an older model during normal escalation.

## 5. Proposed Anthropic judge ladder — request for LPR approval

1. **Default:** `claude-sonnet-5-5:medium`
2. **Default hard retry:** `claude-sonnet-5-5:high`
3. **Complex judge:** `claude-opus-5-5:medium`
4. **Complex hard retry:** `claude-opus-5-5:high`
5. **Exceptional arbitration only:** `claude-fable-5-1:high`

Fable 5.1 is appropriate only when the task is long-horizon/large-context or when Opus 5.5 still returns insufficient/contradictory semantic evidence. It is not a normal judge step because its token price is 2.5x Opus 5.5.

Registered Anthropic fallback pool:

- default-family fallback: `claude-sonnet-5`, then `claude-sonnet-4-6`
- complex-family fallback: `claude-opus-5`, `claude-opus-4-8`, `claude-opus-4-7`, `claude-opus-4-6`
- arbitration fallback: `claude-fable-5`
- `claude-haiku-4-5`: catalogued but **not eligible as judge** because LPR set the floor at Sonnet 4.6.

## 6. GPT chat core surface proposed for 0.11.3

Do not create a second orchestration engine. Extend AirWorker core with:

- `air-worker executor list -product <root> [-json]`
  - returns primary tiers, fallback registry, exact vendor/model IDs and supported effort.
- `air-worker executor dispatch -product <root> -class bulk|standard|complex|hardest -task-file <path> [-json]`
  - core selects the approved primary model and uses existing receipts/budget/session provenance.
- optional operator override: `-tier <approved-tier>`
  - only registered tiers accepted.
- GPT windows call the same commands with existing `actor-kind=gpt-window`/session provenance. No model-specific shell command is exposed as a separate workflow.

The existing semantic judge remains read-only and moves from hard-coded `sonnet` alias to the approved judge policy.

## 7. Release implementation after explicit LPR approval

Target: AirWorker 0.11.3.

Implementation scope:

1. versioned model-policy schema: primary executor ladder + fallback registry + judge policy;
2. exact model IDs; no convenience alias for active 4.6+ models;
3. central fallback selection only for `vendor_limit` / unavailable, never as evidence of model failure;
4. GPT `executor list/dispatch` core commands using existing runner/receipt/session primitives;
5. Claude judge preflight checks user-level credential presence without logging it and performs a minimal normal-mode call; no account login;
6. preserve existing budget ceilings and LPR gates.

Regression gates:

- config/parser/unit tests;
- `check-ladder-reachable` extended to every registered primary/fallback model;
- routing tests proving executor primary path is OpenAI-only;
- judge tests proving semantic path is Anthropic-only;
- deterministic vendor-limit fallback tests;
- GPT principal/core-command smoke with signed session and durable receipt;
- real one-line Codex smoke for `gpt-6-luna`, `gpt-6-sol`, `gpt-6-astra`;
- real Claude smoke for `claude-sonnet-5-5`, `claude-opus-5-5`, and one acceptance call for `claude-fable-5-1`;
- full Go tests, vet, selftest, plugin, shell, boundary, reproducibility, package regression;
- two-node acceptance on SRVLM01 and AIR-ENV-002 if the same 0.11.3 release process as 0.11.2 is retained.

## 8. Rollback

Rollback target: `air-worker--v0.11.2` plus the exact pre-0.11.3 `run-config.json`.

Requirements:

- model-policy schema remains backward-compatible or ignored by 0.11.2;
- no credential migration or account login;
- no destructive state migration;
- install previous signed package through the existing release/update path;
- verify version, binary hashes, selfcheck, tray, runner resolution and GPT session status;
- restore old ladder config byte-for-byte if acceptance fails;
- retain failed 0.11.3 receipts for diagnosis.

## 9. Approval requested

Approve or amend the proposed policy before any implementation/config change:

**Executors:** GPT-6 Luna -> GPT-6 Sol -> GPT-6 Astra as normal route; GPT-5.5 and GPT-5.6 family registered as fallbacks.  
**Judges:** Sonnet 5.5 -> Opus 5.5; Fable 5.1 only exceptional arbitration; Sonnet 4.6 is the minimum allowed judge fallback; Haiku 4.5 excluded from judge eligibility.
