# AirWorker 0.10.7 release acceptance — 2026-09-17

Scope: steps 87–88 — safe Codex writable directories, enforced script-first planning and native Codex orchestration.

- K72–K74: PASS. The release covers validated Codex `add_dirs`, the script-first planner rule, kernel-managed Codex orchestration, exact process-start receipts and the active-session `Agent` bypass guard.
- `go test ./...`: PASS (`airos/air-worker` and `airos/air-worker/cmd/air-worker-tray`).
- `go vet ./...`: PASS.
- `git diff --check`: PASS; Git reports only the repository's existing CRLF conversion notices.
- Plugin/version checks: PASS. `tools/check-plugin.ps1` verified both manifests, seven hook handlers, portable paths and binary version `0.10.7`; `tools/check-goal-file.ps1` passed 28/28; `tools/check-reproducible.ps1` passed 40/40.
- Live add-dir smoke: PASS. A direct Codex `gpt-5.6-luna`/medium run using the release `--add-dir` arguments wrote `AIRWORKER_F_OK` to `F:\_airworker-0107-smoke\marker.txt` and `AIRWORKER_E_OK` to `E:\_airworker-0107-smoke\marker.txt`, exit code 0.
- AirWorker core roster: PASS. Receipts prove two read-only reviewers and one workspace-write leader, all `runner=codex`, `provider=openai`, `model=gpt-5.6-luna`, `effort=medium`, `process_started=true`, status `DONE`. `agents_requested/started/completed = 2/2/2`. The task packet contains no nested host-Agent instruction.
- Opposite-vendor semantic transport: PASS through process start. Claude received the semantic packet on stdin and returned the account weekly-limit response; provider quota is outside the 0.10.7 release gate.
- Ponytail review: PASS — `Lean already. Ship.` after removing the duplicate nested roster, circular model-written PASS gate and Windows long-argv transport.
- Build: Go `1.27.0 windows/amd64` (0.10.6 used Go 1.26.8; recorded toolchain drift). CLI SHA-256 `392ED26956BE9EE062BDD897D6258D8050CA7FC6A118E1E84BCA6DC10742331E`; tray SHA-256 `013F984AD116003584FB5A04115B397C36268F02CADD687CD2175214074B76FA`.
- Historical broad checks outside steps 87–88 retain their existing open findings: `check-engine.ps1` (single judge call), `check-boundary.ps1` (`AIR_LLM_ROUTER_PY`) and `check-planner.ps1` (no product planner answer). None is changed or claimed closed by 0.10.7.
- Release publication and installation: PASS. Commit `fcf7116dd7be0c06d0eeb4570dcdea1d83606844` is on `origin/main` and `origin/codex-add-dirs-0.10.6`; annotated tag `air-worker--v0.10.7` resolves to that commit. Claude and Codex both report `air-worker@air-plugins` 0.10.7. Source, Claude cache, Codex cache and live CLI SHA-256 are all `392ED26956BE9EE062BDD897D6258D8050CA7FC6A118E1E84BCA6DC10742331E`; live tray SHA-256 is `013F984AD116003584FB5A04115B397C36268F02CADD687CD2175214074B76FA`. Non-elevated install/status passed, OpenCode 1.18.31 and Router bridge verified, Start Menu registered, PATH registered, tray stopped and autostart removed.