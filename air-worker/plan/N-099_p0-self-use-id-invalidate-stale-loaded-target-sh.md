---
id: "N-099_p0-self-use-id-invalidate-stale-loaded-target-sh"
title: "P0-SELF-USE-ID: invalidate stale loaded target SHA before procedure_used"
parent: "N-093_p0-self-use-verifiable-outcome-for-newly-learned"
trigger: "N-093 source review 09.10.2026: selected procedure_used from a remembered loaded target@SHA without checking current managed file bytes; after learning procedure is rewritten stale context could produce a false usage receipt"
owner: "gpt-airworker-release-20261009"
done_when: "Before accepting an execution-unknown trigger or correlated procedure_used, verify current managed-skill path is local, canonical, regular, within authorized prefix, without symlink/reparse path, bounded, and SHA equals current SessionStart target@SHA; reject stale/foreign/unsafe identities without usage events, preserve all existing positive legacy proofs. Dedicated before-trigger and after-trigger stale-SHA tests, unsafe path negatives, full Go test/vet and exact source/receipt commit, native validate/plan-lint, no live install/release or direct LEARN edits."
status: "closed"
return_to: "N-093_p0-self-use-verifiable-outcome-for-newly-learned"
created_at: "2026-10-08T17:06:23.9121978Z"
updated_at: "2026-10-08T17:15:47.5257263Z"
receipts:
  - "docs/receipts/AIRWORKER_N099_STALE_SHA_GUARD_20261009.json"
---

# P0-SELF-USE-ID: invalidate stale loaded target SHA before procedure_used

- Родитель нити: N-093_p0-self-use-verifiable-outcome-for-newly-learned
- Владелец: gpt-airworker-release-20261009
- Готово когда: Before accepting an execution-unknown trigger or correlated procedure_used, verify current managed-skill path is local, canonical, regular, within authorized prefix, without symlink/reparse path, bounded, and SHA equals current SessionStart target@SHA; reject stale/foreign/unsafe identities without usage events, preserve all existing positive legacy proofs. Dedicated before-trigger and after-trigger stale-SHA tests, unsafe path negatives, full Go test/vet and exact source/receipt commit, native validate/plan-lint, no live install/release or direct LEARN edits.
