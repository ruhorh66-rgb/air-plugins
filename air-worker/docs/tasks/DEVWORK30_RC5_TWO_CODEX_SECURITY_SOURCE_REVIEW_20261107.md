# Native independent source/security review · AirWorker 0.11.11-rc.5 candidate

**Owner:** Dev Work 30 / SRVLM01.
**Scope:** SOURCE_ONLY. The native tool supplies the exact Git SHA, task SHA and existing N-130 done_when. Do not claim signed release, installed E2E, or closure of N-130/N-123/P0.

Review the actual clean source (not historical descriptions) at that exact Git HEAD, specifically:

1. `cmd/runner_codex.go`: prove that `invokeCodexOrchestrated` starts **exactly two independent Codex coding subagents** on the ladder; each is read-only to protect the shared source, but must author real unified diff code and get individual native job receipts. A narrative-only or missing-patch response must **fail closed without leader dispatch**. Check whether malformed, unsafe or spoofed patches could yield false PASS; identify P0/P1 if so.
2. `cmd/operational_run.go`, `cmd/loop.go`, `cmd/model_policy.go` and `run-config.json`: exact two-subagent mandatory mode in normal native route AND in N-139 operational-goal path, no optional/silent single-executor downgrade, Codex GPT-6 Luna→GPT-6.1 Terra→GPT-6 Sol→GPT-6.1 Sol→GPT-6 Astra (low/medium/high), one writing leader, causal tier escalation only.
3. `cmd/hook.go`, `.codex-plugin/plugin.json`, `.hermes/plugins/air-worker/skills/operate-air-worker/SKILL.md`: all startup/host routes give the same safe mandatory two-subagent advice and independent Anthropic Sonnet/Opus judge.
4. `cmd/runner_codex_orchestration_test.go`, `cmd/runner_codex_live_orchestration_test.go`, `cmd/native_route_contract_test.go`: check that negative regressions cannot credit two read-only **commentaries** as coding, and the source-only live fixture proves actual Codex code patch proposals+leader edit, not full P0.
5. **Release S→B→R:** check N-130 source/security constraints. Published `air-worker--v0.11.11-rc.4` had a bad source distribution (stale binary 0.11.9, marketplace pinned rc.2). Its signed release/manifest must NEVER be reused for rc.5. Requiring an accurate VCS-clean CLI+tray package B, payload SHA, independent verification and Ed25519-signed new tag is necessary; source PASS alone must not authorize install or P0 close.

Output exact `PASS_SOURCE_ONLY` only if safe within the code slice; otherwise `FAIL_P0`, `FAIL_P1` or `NOT_PROVEN`. Verify original TASK_READ_OK and give up to three exact file/line code citations. Preserve true limitations: no cross-GPT-session G07, no causal G06, no final N123 full G05, no signed rc.5 or installed accept. Do not modify files, run installers, alter keys, rerun models, or write plans/LEARN.
