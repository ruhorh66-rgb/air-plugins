# AirWorker rc.4 · independent source/security release review (read-only)

Product: AirWorker. Owner: Dev Work 26. Source commit for review: b659d8684b96662567cc53290019af8ca92c3b98, released artifact source from GitHub branch dev/airworker-native-orch-pilot-20261104.
Trigger: LPR authorized publication and installation of AirWorker 0.11.11-rc.4; review the code before promotion. This is a reviewer-only native AirWorker executor assignment, not a 2-subagent orchestration acceptance.

Review ONLY real source files inside this product root, report exact path:line evidence, and do not write/change any file or launch commands outside read-only inspection. Read all:
1. cmd/bypassguard.go, cmd/hook.go, hooks/hooks.json: predeclare SessionStart, active-only PreToolUse deny, bypass/false positives, host transport limitations.
2. cmd/model_policy.go, cmd/loop.go, cmd/executor.go: two native parallel Codex, Hermes loop canonical flags, lock/protection, alternate config veto.
3. cmd/semantic.go, cmd/plan_review.go, cmd/claude_route_preflight.go: opposite-vendor Anthropic judge and inherited OAuth/Headroom assumptions.
4. cmd/runner_codex.go and cmd/release_slice_runner.go: actual parallel runner and historical R0.11.9-05 deny.
5. .claude-plugin/plugin.json, .codex-plugin/plugin.json, .hermes/plugins/air-worker/__init__.py, .hermes/plugins/air-worker/skills/operate-air-worker/SKILL.md: three host contracts.
6. cmd/update.go, cmd/update_identity.go, cmd/update_windows.go, cmd/distribution_identity.go: official signed update and rollback. In particular identify whether the frozen package binary lacking vcs.revision could pass update apply, and whether the native publisher private signing key is required.

Judge criteria: P0 any security bypass, mishandled permission or foreign service, unbounded model execution, false reviewer PASS, inability to install via official signed channel, or broken Hermes route; P1 wrong subscriber auth/session startup, non-portable installed plugin, wrong reviewer, false source provenance or restore gap. Tests source-only, no API or PAYG, no installs, no Git mutations.

Your final reply MUST contain:
- Terminal label PASS_SOURCE_ONLY / FAIL_P0 / FAIL_P1 / NOT_PROVEN;
- 3-8 factual source findings (file paths and function names, severity, exact minimal fix) with read evidence;
- Distinguish checked source from runtime unproven; never claim actual 2/2 or installed hooks if absent.
- Explicit 'TASK_READ_OK' only if you actually read this task file and listed sources; if you could not read it, say 'TASK_READ_FAIL' and return NOT_PROVEN.
- Do not call codex app-server; this task is dispatched via the installed native AirWorker executor. Do not silently use Astra or paid API.
