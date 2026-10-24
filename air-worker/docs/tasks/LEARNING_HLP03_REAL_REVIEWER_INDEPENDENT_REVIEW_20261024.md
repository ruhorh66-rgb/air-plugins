# HLP-03 independent review: evaluator, frozen first-run answers, and evidence

Use existing checkout on SRVLM01 only, installed Ponytail in full mode, native tools. This is an independent REVIEW, not a model-evaluation rerun. Read all 12 provider answers yourself. Do not infer PASS from test exit code or coordinator summary.

Allowed write ONLY: docs/receipts/LEARNING_HLP03_REAL_REVIEWER_INDEPENDENT_REVIEW_20261024.json
No edits to source, tests, corpus, labels, protocol, native PLAN/N/LEARN, runtime, wiki, deps or foreign WIP. No provider evaluation, install, broad test run, commit/push, other node or service action. You may run bounded read-only shell/JSON/hash inspection. Do not rerun the real reviewer.
Evidence root: .air-worker/learning-loop-20261024/hlp03-live-20261024-baseline
Read launch.json, terminal.json, evidence/summary.json, each case result.json + adapter-raw.json + native .out and .receipt.json, resources.jsonl, and post-run-audit.json if present.
Inputs and frozen gates: cmd/testdata/hlp03_reviewer_corpus_20261024.json, docs/tasks/HLP03_REVIEWER_GOLD_20261024.json, HLP03_CORPUS_FREEZE_20261024.json, HLP03_EVALUATION_PROTOCOL_20261024.json.
Current evaluator: cmd/learn_adapter_live_eval_test.go; wrapper: docs/tasks/run-hlp03-real-reviewer-20261024.ps1.
Production core is unchanged, existing reviewed production-code SHA ca92aa4d2e6c1b8bf68954f1a3701611c3bfa632, source receipt HEAD 95a4377f35948f9396d59cfe86d1e1e5e29e2979 and pinned binary SHA 69343382ba477a74dddd5b86092e31281b45d5d0284d536d4a037fae0aa02021.

Known instrumentation issue to assess honestly: the first real run registered its cleanup check after the first per-case t.TempDir. Go shares that cleanup, so the check ran before Go removed fixture roots; raw run may have Go exit 1 with cleanup_failed:<nil> while reviewer/module outcomes were preserved. The coordinator will retain the original harness as harness-at-run.go.txt with launch SHA, verify all owned roots absent after completion, and fix cleanup registration order without repeating model calls. Do not erase this technical failure or count it as a model decision. Check current source and original run separately.

Independently verify:
- corpus/gold/freeze/protocol and binary/source/harness hashes; exactly 12 first-attempt cases, production reviewer model gpt-5.6-luna / medium, no stub/fallback/replay;
- exact provider decisions from raw output vs adapter canonical output (a core guard rejection must not hide a model error); provider terminal and usage null if unknown;
- every tool call actually logged, no observed source/corpus/gold access; allow own fixtures and vendor Ponytail; note limits of trace heuristic vs filesystem isolation;
- update correct gold target/preSHA, meaningful complete content retaining prior applicability/actions, backup/rollback; no_candidate no new/revised skill; creates supported distinct lesson, unrelated seeds unchanged;
- strict confusion matrix and class/identity score out of all 12, boundary C02/C05/C08 and other 9 as prespecified descriptive groups only; no posthoc relabel/retry;
- separate class disagreements from false merges, unsafe updates, malformed output and lifecycle errors;
- resource sampling, observed peak vs 2 GiB working estimate (not hard OS quota), shared volume reserve, known full DEV-137 NOT_CLAIMED;
- installed runtime/config/native PLAN/release-slices/module pin preserved; existing WIP untouched.

Output valid JSON with schema, reviewed_at_utc, reviewer model/effort, source/harness identities, source_review {verdict,p0,p1,p2,findings}, measurement_review {verdict,limitations,baseline_go_exit_code,cleanup_adjudication}, quality_review {verdict,total,correct,confusion,per_case:[id,expected,actual,class_match,identity_correct,content_verdict,reason],unsafe_update_count,false_merge_count,contamination_count,usage}, production_effect NOT_PROVEN, recommendations.
PASS_THIS_CORPUS requires the frozen protocol, not a relaxed threshold. If disagreements are defensible due boundary granularity, report that explanation while leaving strict gold score unchanged. Do not mark production complete or release-ready.
Return short findings plus the receipt path; leave source unchanged.
