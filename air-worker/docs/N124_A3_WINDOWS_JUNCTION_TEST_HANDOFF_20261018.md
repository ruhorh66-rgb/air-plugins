# AirWorker N-124 · A3 Windows junction regression · 18.10.2026 SGT

**Status:** `SOURCE_ONLY_TEST_JUDGED_PASS / N124 OPEN / A3-1 WAIT_LPR / DEV136 HOLD`.

## Exact frozen evidence

- Branch: `dev/airworker-n124-a3-windows-reparse-20261018`, based on accepted source `540bc7c21eb25febeba8e545b6f54290d09068ff`.
- First test-only commit `ecd46c7957cfe178bd8d58e3c464f143b7d96b4d`; corrected test-only commit `9b1791b0c53d9d5c3a97568b64de601daca3b5b8`.
- **No executable Go source change.** Only `cmd/plan_candidate_junction_windows_test.go` was added/edited. No native `PLAN.md`, `plan/N-*`, `learn/events.jsonl`, live installed product, or worker B touched.
- First host test unexpectedly **PASS RC0**, not RED; existing `candidateNoLinks` already rejected a real Windows junction on SRVLM01. However, the test initially only checked *some* error and could have been a false-positive.
- Independent Codex `gpt-6-sol low` on `ecd46c7`: **FAIL/P1=1** (the fixture root itself might be linked, causing all assertions to pass). Host logs alone did not establish exact attribution.
- Corrected test: check ordinary root/sibling/future leaf accepted; create actual `mklink /J` or fail; verify foreign sentinel through the junction; deny junction, existing foreign leaf, and absent future N-leaf **only if the reported error names the junction itself**. Foreign sentinel unchanged.
- Corrected host suite: 3 main PASS + 3 junction nested PASS, **2 existing tests SKIP** (no linked OS-temp ancestor and no symlink privilege), RC0. Log SHA256 `0e7d17628b4c8bb3bfd2874d332f800741c2e1f9f5ba957dc9ba994ad7704ff7`.
- `go vet ./...` RC0, `go build` RC0; isolated binary SHA256 `6333ad7a6c37d5d9bbc38efe5c5d3e5a933e3a797ea289673b7c2c0078d90d85`. **Not installed**.
- Independent read-only Codex `gpt-6-sol low` on corrected commit `9b1791b`: **PASS, P0/P1/P2 = 0** within the **single test diff only**, result SHA256 `070df32bb080ff45f414b771d13d2bb336f7941760074952bc38fca10ae9ed0d`. Reviewer did not reproduce Go host logs.
- [Machine source-only receipt](receipts/AIRWORKER_N124_A3_WINDOWS_JUNCTION_TEST_20261018.json) contains owner/trigger/done_when/judge, scoped hashes and open safety gates.

## Strict limits and next step

This is additional Windows test coverage, **NOT** a production fix, new GitHub publisher, release acceptance, full Go suite, N124 native close or LPR approval. A3-1 still requires explicit LPR permission for *two named isolated GitHub refs* and a separately approved canonical authority identity. No such refs were created. Full regression and broader Windows symlink cases remain `NOT_PROVEN`. AirWorker installed `0.11.8` unchanged; AirLegal N111 blocked pending N124 and DEV136.

Existing approved A3 source is [`540bc7c`](https://github.com/ruhorh66-rgb/air-plugins/commit/540bc7c21eb25febeba8e545b6f54290d09068ff). Rollback the two **test-only** feature commits without changing production.
