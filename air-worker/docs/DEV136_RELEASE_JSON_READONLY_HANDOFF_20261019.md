# AirWorker / DEV-136 — strict release-sidecar read-only preflight

**Дата:** 19.10.2026 SGT · **Блок:** A / DEV-136 · **Статус:** SOURCE-ONLY JUDGED PASS; native N-124 and DEV-136 `-apply` HOLD.

## Проверенный результат

Owner: AirWorker worker A; curator Voody. Trigger: machine release facts `plan/release-slices.json` were read by preflight after a separate Lstat via unbounded `os.ReadFile`, and Go JSON decoder silently accepted repeated keys. Independent reviewer found Unicode SimpleFold alias and hardlink bypasses; later found pre-open size/mtime P2.

Source branch `dev/airworker-dev136-release-json-gate-20261019`; base `d71c5d7589a2ad913b6f453a33d298a511addc56`, corrective source commits `63c63b9645f4b5f2f30b2433a650319b6c71c944`, `ecc8b02875e6276504724aaec7c70b65fc6a8c7d`, final reviewed code SHA `b6aefa2fe8cf372f47058fe4fd5097a8688c35e5`.

Read-only DEV-136 sidecar reader now bounds actual reads to **1 MiB + 1 byte**, checks initial/opened/readback physical identity, size and modification time, rejects Windows reparse point and hardlinks (`NumberOfLinks != 1`), refuses duplicate effective JSON keys recursively including escaped/Unicode SimpleFold aliases, invalid UTF-8, excessive depth and multiple JSON values. Unknown unique fields remain compatible.

**No write access was added.** `plan header sync -apply` still returns BLOCKED/RC2 with the installed-binary and N-124 authority blockers.

## Exact factual TDD / QA / judge

| Evidence | Observed result | Log/verdict SHA256 |
|---|---|---|
| Duplicate-key TDD RED | 4 ambiguous JSON cases accepted incorrectly, RC1 | `c886c14c560d57a69cdb7a12f45ff14badb1a9c080fdcb0331083d6413495d4f` |
| Unicode SimpleFold TDD RED | 2 incorrect alias accepts, RC1 | `3e6e8eaa79cc67eb03ce7a24eae7413e57bc8b81c6b8fa0b179f2c26021b5504` |
| Foreign hardlink TDD RED | 1 incorrect accept, RC1 | `c85842862d4aa636aa1fc6a842f28f267618fd70b3825da21a8b1f27b0db14de` |
| Unicode fold vs Go EqualFold | 11 nested PASS, RC0 | `e3e95167d5858fafdcb5fcee57f003d25aaa3575653288766a79608aa9da09b5` |
| Pre-open same-file size/mtime test | 3 nested PASS, RC0 | `5d68911d9368b1add749fb76d9430eecce9a8f8b32562c3661b1f0359d8b70b6` |
| Final `^TestPlanHeader` | 30 main + 57 nested PASS, RC0; 1 Windows symlink privilege SKIP | `65306ea7801769689d1c69d8a5af148ed9abed16e71e5342adf2f741b357cfdd` |
| Go vet / build | RC0 / RC0; candidate exe only on R: | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` (both empty logs) |

- Independent Codex `gpt-6-sol medium` reviewed `63c63b9`: FAIL, 1 P1 Unicode fold + 2 P2; verdict digest `aa376a8b7865cc20328c47cf530feb4ff1d26556204123680a600ba1e9567dd3`.
- On `ecc8b02`: PASS, P0/P1=0, P2=1 pre-open generation drift; digest `e3e2a10ceb422804eaa6e756a48a2c8499a2423b31ef98cd4fa667904c90bf49`.
- **Final independent read-only `gpt-6-sol medium` on `b6aefa2`: PASS, P0/P1/P2=0**, digest `ac7343667bca2f6f2b51da57a9778d14605c39a8b355efeae745993e51be1eb8`. Reviewer examined source but did not independently rerun host QA.

Machine receipt: [DEV-136 JSON source acceptance](receipts/AIRWORKER_DEV136_RELEASE_JSON_READONLY_SOURCE_ACCEPTANCE_20261019.json) (owner, trigger, done_when, judge, receipts, log SHA, rollback, limits). Host logs and independent verdicts in `R:/-4-/air-worker/work/DEV136_RELEASE_JSON_20261019`, initial duplicate-key RED in `R:/-4-/air-worker/work/N124_WORKER_A_20261018`.

## Integration boundary — do not overwrite the native plan

**Important factual divergence:** this DEV-136 source branch is based on an older product plan (68 nodes, no native N-124 and no `plan/release-slices.json` currently in its worktree); the newer N-124 feature plan contains 125 nodes including N-124. Thus current source-only positive tests use a disposable machine sidecar. Missing canonical release sidecar must return BLOCKED. Never merge/copy the DEV-136 `PLAN.md`, `plan/N-*` or LEARN into the newer product by hand.

**Remaining gates:** current native plan/release-slices reconciliation through supported AirWorker core and one sequential owner, independent installed binary attestation, global durable generation and source-of-truth, full Go repository regression (only `^TestPlanHeader` passed here), accepted separate release/install approvals. N-124 remains OPEN in the newer product, AirLegal N-111 OPEN; no unauthorized native close.

**Rollback:** revert only feature commits on branch `dev/airworker-dev136-release-json-gate-20261019`. Installed AirWorker **0.11.8** SHA256 `1b4008af05110214934d46c0ab8c5236341cd1c699f54efb895c2f9f64a94a94` remains unchanged. Foreign WIP of Hermes (4 staged), worker B and dirty AirLegal product untouched. No UAC, reboot, stop service, release or install.

**Next:** source-only preflight against an explicitly selected current native plan without mutating plan data; then actual reviewer/QA for approved integration. Do not request a new canonical GitHub branch merely to coordinate two workers.
