# AirWorker 0.11.3 N-015 model policy acceptance — 2026-09-29

Node: SRVLM01
Plan node: N-015_0-11-3-approved-model-policy-and-effort-caps
Source commit: 939929e

## Accepted facts

- release ladder: script -> gpt6-luna low/medium/high -> gpt6-sol low/medium/high -> gpt6-astra low/medium/high.
- no release rung uses xhigh, max or ultra.
- registered OpenAI/Codex fallbacks: gpt-5.5, gpt-5.6-luna, gpt-5.6-terra, gpt-5.6-sol.
- vendor-limit fallback registry is separate from normal escalation.
- legacy haiku/sonnet/opus plan labels resolve to GPT-6 Luna/Sol/Astra respectively; an explicit forbidden effort such as sonnet:max is rejected rather than silently lowered.
- judge policy is versioned as air-worker.model-policy/v1:
  - default claude-sonnet-5-5, low -> medium -> high;
  - complex claude-opus-5-5, low -> medium -> high;
  - arbitration claude-fable-5-1, low -> medium -> high;
  - fallback floor includes claude-sonnet-4-6.
- no new token telemetry was added.

## Verification

Targeted Go tests PASS:
- TestReleaseModelPolicy0113
- TestReleaseJudgePolicy0113
- TestLegacyExecutorTierAliasesMapToOpenAI
- TestSemanticReviewerOppositeVendor
- TestReleaseProfileUsesOpenAIExecutorsAndAnthropicJudges
- TestVendorLimitClassification
- TestPlannerPromptRequiresScriptFirst

Additional:
- gofmt PASS
- go vet ./... PASS
- git diff --check PASS

Transport integration remains in N-016 (Headroom) and N-017 (Ponytail).
