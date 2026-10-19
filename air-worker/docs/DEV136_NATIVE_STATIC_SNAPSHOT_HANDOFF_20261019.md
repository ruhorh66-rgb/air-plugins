# AirWorker DEV-136 · Immutable executable snapshot + independent P1 remediation
**19.10.2026 SGT · SOURCE_ONLY_SCOPED_SOL_PASS / N124 OPEN / DEV136 -apply BLOCKED.**

## Source and result

The DEV136 installed identity read-only preview previously had two separate source failures:
* `da6685f`: native selfcheck could execute an attacker-swapped cache EXE for `version`; independent Sol medium FAIL/P1.
* `8065b68`: execution eliminated, but SHA256 and Go build-info were read via two pathname opens, permitting SHA(A) with revision(B) after a swap; independent Sol medium FAIL/P1.
* **Final fix [`6a11beb`](https://github.com/ruhorh66-rgb/air-plugins/commit/6a11beb9a8fc1b3265c255167bf9b704d5c4441a)** hashes and parses static Go VCS metadata from a single bounded immutable byte snapshot, with no cached EXE execution.

The final source commit changed only `cmd/plan_header_installed_native_reader.go`, `cmd/plan_header_installed_static_report.go`, and the new `cmd/plan_header_snapshot_generation_test.go`. A disposable real Git repository with two commits produced valid Go EXEs A/B with different SHA and revisions at the *same path*: after replacing A with B, decoding A's saved snapshot remained strictly A. **1 PASS RC0**, log SHA256 `0555b0020daef86b5e71406cea6023712a6aa4813f4ee329bb68a683b9833256`.

Final `^TestPlanHeader` scoped suite: **34 main PASS, 84 nested PASS, 2 SKIP, RC0**, log SHA256 `6a17d467371db480bf4222e528aa3b28ab4c91ae5e812fb3d90221bde57cafc5`. Go vet/build, native plan-lint/validate RC0. Source-only binary SHA `a0d262b0fd92573780f537691e16d1199a338157a02998ced74aad532b9b1dd3`, not installed. **Full repository Go regression not asserted.**

## Independent semantic acceptance

Read-only Codex `gpt-6-sol medium` reviewed exact frozen `6a11beb9a8fc1b3265c255167bf9b704d5c4441a`: **PASS, P0=0, P1=0, P2=5**; verdict SHA256 `daab3e54f355c178cf0888da7cbb531be6e7a7ac44bcee6a877859c57f8f6b3d`. It inspected source but did not rerun host tests. Earlier failure at `8065b68` was preserved, verdict SHA `a0f1bcb0884df33376a6409e69678ab3f39d7d46e6cca7d643a55376a9867bf1`.

The remaining five P2 concerns are explicitly **NOT accepted as production security**: mutable in-place file edits with restored metadata, environment-derived installation roots, unsigned local marketplace registration, partial inactive-profile inventory, and unbounded cache `payloadSnapshot`. None is a release approval.

## Current native integration boundary

Prior actual N124 plan `-dry-run -json` for source `8065b68` returned **BLOCKED, RC2, changed=false**, confirming installed 0.11.8 SHA `1b4008af05110214934d46c0ab8c5236341cd1c699f54efb895c2f9f64a94a94`, with `active_release_not_proven` and `N124_global_git_generation_not_accepted` still open. PLAN.md, N124 and learn journal SHA were unchanged. The new `6a11beb` has no permission to write or install. Source-only attestation does not equal a signed artifact provenance.

**Owner:** AirWorker line A; **trigger:** independent source judge P1; **done_when:** one-handle bound snapshot, Git A/B swap fixture, scoped regression and independent frozen-code judge PASS; **judge:** Go host logs + independent Sol medium; **receipt:** [machine JSON](receipts/AIRWORKER_DEV136_NATIVE_STATIC_SNAPSHOT_SOURCE_ACCEPTANCE_20261019.json); **rollback:** revert only own three feature commits, installed AirWorker 0.11.8 unchanged.

**Next measured step:** bound plugin-cache payload scans and examine inactive registrations; then native active release and N124 CAS, full Go regression, independent acceptance. B/N093 is independent; no new protected GitHub canonical branch required for two workers.

Vendor documentation: [debug/buildinfo.Read](https://pkg.go.dev/debug/buildinfo#Read), [io.LimitReader](https://pkg.go.dev/io#LimitReader).
