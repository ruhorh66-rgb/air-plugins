---
id: "N-130_rc4-release-p0-source-reviewer-fixes-and-transac"
title: "RC4 release P0 source reviewer fixes and transactional rollback"
parent: "N-129_native-route-enforcement-at-hook-and-cli-boundar"
trigger: "LPR 05.11.2026 granted publication+install of rc4; native Codex Sol medium independent source reviewer FAIL_P0: compound marketplace command escapes bypass guard, failed Claude process may yield semantic PASS, updater cache rollback incomplete; update requires signed Ed25519 channel and authentic vcs.revision"
owner: "Dev Work 26 / SRVLM01"
done_when: "Fix two reviewed P0 with strict negative tests and no permissive alternate path; transactional update rollback protects both own Claude and Codex caches and binaries, preserves runtime/private state, with injected failure/recovery tests; build VCS-stamped assets from exact clean Git source and accept version/modified=false; full narrow regression, vet, three host plugin QA and independent source judge PASS on exact SHA; publish immutable GitHub prerelease and signed Ed25519 channel through native publisher only when signer owner authorizes supported path; install through official update transaction with existing verified rc2 rollback; verify live version/config/hook delivery and return exact machine receipt; N126/Hermes P0 stay separate OPEN"
status: "open"
return_to: "N-129_native-route-enforcement-at-hook-and-cli-boundar"
created_at: "2026-11-05T00:59:55.7987773Z"
updated_at: "2026-11-05T00:59:55.7987773Z"
receipts:
---

# RC4 release P0 source reviewer fixes and transactional rollback

- Родитель нити: N-129_native-route-enforcement-at-hook-and-cli-boundar
- Владелец: Dev Work 26 / SRVLM01
- Готово когда: Fix two reviewed P0 with strict negative tests and no permissive alternate path; transactional update rollback protects both own Claude and Codex caches and binaries, preserves runtime/private state, with injected failure/recovery tests; build VCS-stamped assets from exact clean Git source and accept version/modified=false; full narrow regression, vet, three host plugin QA and independent source judge PASS on exact SHA; publish immutable GitHub prerelease and signed Ed25519 channel through native publisher only when signer owner authorizes supported path; install through official update transaction with existing verified rc2 rollback; verify live version/config/hook delivery and return exact machine receipt; N126/Hermes P0 stay separate OPEN
