# N-126 native orchestration: Codex executor 1 (complex)

Owner: Dev Work 25 / AirWorker. Parent node: N-031_learn-2-события-ядра-и-автомат. Actual next historical native release step: R0.11.9-05.
LPR trigger: on 2026-11-04 SGT, authorized exactly TWO sequential Codex/OpenAI executor units and independent Anthropic reviewer, ONLY through installed AirWorker native core.
Scope: read-only source investigation, no product logic changes yet. Deadline: one bounded source-reading pass and one evidence artifact.

Read actual files in THIS product root, first:
- this task file;
- PLAN.md, plan/release-slices.json;
- cmd/release_slice_runner.go and cmd/loop.go;
- cmd/learn_adapter.go and cmd/learn_shared.go;
- run-config.json (do not expose secrets or tokens).

Deliver one factual markdown artifact at docs/receipts/N126_ORCH_EXEC1_R05_SOURCE_AUDIT_20261104.md. It must include the read path and Git HEAD, the current active release and R05 state as read, the exact function/gate that prevents native loop from executing R05, the smallest justified next native code or plan action, explicit factual gates to keep OPEN, and any needed follow-on guidance for executor 2. Quote only short, exact source code snippets and real evidence; state NOT_READ if a source cannot be opened.

Do not modify PLAN.md, plan/N-* or LEARN/runtime files except by native core (no native node changes asked by this task); do not install/update/reboot/UAC/ACL/stop services; do not run Go tests or vet, git archive, or tools previously denied by platform security; do not reroute a denied operation through another account/tool/provider. Do not make external API/PAYG calls. Do not write a false PASS. This first step is read-only analysis, not a release.
