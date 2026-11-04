# N-126 executor 1 — bounded R05 source audit

Date: 2026-11-04. Result: SOURCE_ANALYSIS_COMPLETE; R05 execution/release NOT_PROVEN. This is an evidence note, not a native terminal job receipt or independent judge verdict.

## Read identity and limits

Product/read root: `D:\Devbox\AirWorker\worktrees\awlrn-orch-pilot-20261104\air-worker`.
Git HEAD: `c43b0e3168e9a41c8358b229aa0cd2b61a02930e`.

Read the executor-1 task first, then opened all seven required product files below. Initial bulk output was truncated; the findings use subsequent targeted source excerpts, JSON projections and searches. This was a bounded investigation, not an exhaustive review of every line of PLAN/loop. No required file failed to open. Configuration was parsed locally and only selected non-secret fields displayed.

| Required source | SHA-256 of working-tree bytes |
|---|---|
| `PLAN.md` | `96003980F0B624AA7915F50763B596835D1A02CD30EF5F34776421AA5136A705` |
| `plan/release-slices.json` | `811A37794842D8ED575F478D0A84665579DAEE5F5AFE94E5E4AD6E9E7627AB9A` |
| `cmd/release_slice_runner.go` | `5177E7D18C6167F943091790042E6F61CD0AE238238629B7557B1AEB8BCCD8F0` |
| `cmd/loop.go` | `D90BF4FEBB8937AA82640DBD14415E22EFD51BD1377728BB9189E417CA286CD8` |
| `cmd/learn_adapter.go` | `51ACF40A3AA43FAD4BA8BFAE1F0C4978F8594B449F8E1B5C73D04D38467FD434` |
| `cmd/learn_shared.go` | `FD81C99DB9DCC22C023C5C770B1968E84494C2B09CF65B16D60012D181A7EB96` |
| `run-config.json` | `24F558DB773E19D27F2D2F1F29C741BAC985045DF7541E45D32DC50F9AA9452C` |

Additional targeted reads: `cmd/release_slices.go`, `cmd/release_slice_historical_fixture_test.go`, `cmd/main.go`, `cmd/go.mod`, and the executor-2 task. Searched release-loop callers under `cmd/*.go`. No root AGENTS.md or tracked AGENTS.md was found by the performed checks. Graph generation was omitted under the task's one-artifact/read-only restriction; `graphify-out/graph.json` was absent.

Initial Git status already contained modified `PLAN.md`, modified `learn/events.jsonl`, and untracked executor-1/executor-2 task files plus `plan/N-126_native-two-codex-and-anthropic-reviewer-orchestr.md`. These are pre-existing, not this audit's edits. HEAD alone therefore does not identify the working-tree plan; the hashes above do.

## Current release state as read

`plan/release-slices.json` has schema `air-worker.release-slices/v1`, active release `0.11.9`; R01–R04 are `closed`, R05 is `open` with kind `qa_submit`, R06/R07 are `planned`. R05 names `docs/receipts/RELEASE_0_11_9_05.json`, budget 10 minutes. These are recorded statuses, not independently revalidated completion evidence.

R05 requires N-070 publication/pin/integration evidence and native N-071 acceptance reconciliation before immutable source+plan freeze, followed by one receipted QA job and recovery by job_id readback. The sidecar explicitly records N-070 `NOT_PROVEN` and N-071 `OPEN`, both required before R05. PLAN.md:54 agrees on those pre-freeze blockers. PLAN.md:18/40/52 still describe R02 as the next step: stale narrative, not authority to rewind the machine plan. `validateReleaseSlices` assigns the active open step to `ActiveStep` and `NextStep` (`cmd/release_slices.go:281–304`). No native validation command was executed.

Source identity differs from historical release identity: `cmd/main.go:41` reads `version = "0.11.11-rc.3"`. This is the source default, not a measurement of the installed executable or build-time overrides. Historical fixture code explicitly preserves exact-version refusal for later binaries; its listed later pair is rc.2/learning 0.1.1, so it cannot establish rc.3 QA success.

## Exact native-loop refusal

`cmdLoopWithReleaseSliceRunner` in `cmd/loop.go:242–264` inspects the sidecar and calls `releaseSliceLoopSupported(releaseState, version)` before dispatch. An error returns rc2 and forbids numeric PLAN fallback. Earlier config/judge/model-policy checks can also refuse; they were not executed here.

The decisive R05 exclusion is `cmd/release_slice_runner.go:43` in `releaseSliceLoopSupported`:

```go
if state.ActiveStep.ID != nativeReleaseSliceR02 && state.ActiveStep.ID != nativeReleaseSliceR03 && state.ActiveStep.ID != nativeReleaseSliceR04 {
```

For otherwise valid R05 state the resulting diagnostic is:

```text
native loop currently supports only active R0.11.9-02, R0.11.9-03 or R0.11.9-04; found "R0.11.9-05"
```

This is a source-derived expected diagnostic, not captured runtime output. Three additional constraints make an allowlist-only change incomplete:

1. Lines 47–50 also require plan/candidate version `0.11.9`, open status, and kind `source` except R04 `test`. R05 `qa_submit` fails this condition, and the current source version differs.
2. `cmdReleaseSliceLoop` (lines 54–69) rechecks the gate and only dispatches R02/R03/R04. There is no R05 switch case.
3. `loop.go:259–262` rejects `-whatif`/`-plan-only` before the runner; they are not a safe route to execute or preview R05.

Caller search also found this shared gate in native capture and R03/R04 runtime paths. Loosening it globally could let historical handlers run with an unintended version. Preserve that protection.

## Smallest justified next action

Do not reopen R02 or relax the historical gate to make the loop advance. The immediate justified follow-on is executor 2's bounded LEARN reviewer investigation, independent of obsolete R05 execution. No policy, judge, plan or runtime change is needed for this audit.

For eventual historical R05 work, first obtain N-070 evidence and reconcile N-071 through native AirWorker with node/index/receipt readback. Resolve which exact historical candidate is authorized before implementation. If R05 execution is still required, the minimum complete future code scope is a dedicated `qa_submit` runner, explicit ID/kind dispatch and version contract, pre-freeze evidence checks, and reuse of native durable job submit/readback. Changing only the allowlist is unsafe and insufficient. Existing job primitives were NOT_READ in this pass, so this is a scoped design requirement, not a claim that a particular helper is ready. Future targeted checks must cover blocked prerequisites, wrong candidate, unsupported step/kind, duplicate submit/recovery, and no numeric fallback. No such implementation or QA is authorized/executed here.

## LEARN observations and executor-2 handoff

`reviewerCatalog` (`cmd/learn_adapter.go:59`) already permits up to 64 indexed procedures; each body is read with a 64 KiB limit, SHA checked against the index, and aggregate body bytes capped at 128 KiB. It fails closed rather than truncating. The nearby comment's 37 items is not a live catalogue count. Do not propose raising the count without reading the current regression fixture and actual failure evidence.

`sharedLearningStop` (`cmd/learn_shared.go:518`) records `kind: run_completed`, `class: completed-turn`, `source: host-finalize`, and `"defer_review": true` through shared-module `observe`; duplicate conflicts are tolerated. `sharedLearningConfig:417` supplies a reviewer callback under a product reviewer lock. These functions do not themselves call the R05 runner. Whether this task's actual host completion reaches them and resumes review is NOT_PROVEN here; hook callers, task routing and `cmd/learn_review.go` were NOT_READ. Executor 2 should trace that path as its own task requires.

`cmdLearningReviewerAdapter` rechecks owner, settings and catalogue after model output and rejects changed state. `invokeLearningReviewerCodex` (`cmd/learn_adapter.go:478`) uses the native receipted Codex runner with role `learning-reviewer`, sandbox `read-only`; this learning reviewer is not the independent Anthropic reviewer for this pilot.

Safe config projection: `learning.auto_apply=false`; `orchestration.enabled=true`, `subagents=2`; default/complex/arbitration judge models are Claude Sonnet/Opus/Fable. `semantic_judge` separately specifies release `0.11.9`, Codex `gpt-6-sol`, effort `medium`, with a comment requiring matching goal release. There is no top-level `release_version` field. Goal state and effective routing were NOT_READ, so do not infer an effective release override or change it. Shared learning settings are selected via `learning-module.json` in the source; runtime configuration was NOT_READ and `auto_apply=false` alone does not prove its effective behavior.

Executor 2 must start only after executor 1 has a terminal native job receipt, as its task specifies. Read this artifact and its own task, inspect the catalogue fixture, review path and N-031, and propose one evidence-backed source-only test/fix. This audit neither launches executor 2 nor supplies the separate Anthropic verdict; the native orchestrator owns those actions.

## Gates kept open and verification limits

- N-070 remains NOT_PROVEN: exact published learning 0.1.0 tag/SHA, consumer pin and publication-gate integration regression not established.
- N-071 remains OPEN: native acceptance reconciliation and node/index/receipt readback not established.
- R05 stays OPEN: no candidate freeze, job_id, QA exits/JSON, security/smoke or rollback dry-run evidence produced.
- R06/R07 remain planned: no same-SHA independent acceptance or release authorization established; no tag/release/install performed.
- Real task completion → deferred review → safe apply → fresh-process use/outcome/effect remains NOT_PROVEN by this pass. Prior closed slice statuses are not new evidence.
- The pilot's two terminal executor receipts and independent Anthropic review remain for the orchestrator; this artifact cannot stand in for them.

Only this Markdown receipt was written. No Go tests/vet/build, native loop or state transitions, external API/PAYG calls, policy/judge changes, privileged operations, services, installation, commit or publication were performed. No platform-security rejection was bypassed. Runtime receipts, installed version, live catalogue and external publication evidence were NOT_READ. Risk: static source/config findings do not prove installed behavior or release readiness.
