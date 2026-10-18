# AirWorker N-124 · A3-2 Windows native handle generation fence (source-only)

**18.10.2026 SGT · SRVLM01 · SOURCE_ONLY_SCOPED_JUDGED_PASS / N124 OPEN / PRODUCTION AUTHORITY HOLD**

Source branch `dev/airworker-n124-a3-handle-fence-20261018` based on accepted mid-read fix `f0a504c48607934f9900ab076d3590bbae178596`. Implemented two new Windows-only source/test files; executable production `plan node new/close` routing unchanged. Frozen reviewed code/test SHA **`12f1db361b3ad803e3c9c4dc83a3552b13c46c4c`**, development commits `7c1fa93` → `3fe560e` → `12f1db3`.

## Verified scope

`holdN124ReplayProjection` accepts only a self-declared `isolated-fixture` inside OS temp; this self-declaration is **not a trusted publisher grant**. The Windows prototype pins NTFS/ReFS ancestor, native PLAN, all plan/ and learn/ regular files and its own `plan-authority.json` with read-only, no-share-write/delete handles and verifies physical identity, bounded original bytes and directory membership. It rejects pre-opened writer, hardlink, junction, oversized native file, missing/production-mode authority. It allows unrelated foreign WIP and releases handles after Close. Tested independent child-process writer returns `ERROR_SHARING_VIOLATION (32)`; rename/delete of held native files denied.

**Important discovered limit:** Windows allows creation of new child N-ID despite a directory handle. A persistent foreign N-ID fails `Verify`; transient create/delete ABA and mutations after return remain possible. This is not a durable global generation/CAS lock or a production permission.

## Source-bound receipts

| Evidence | Fact |
|---|---|
| TDD before helper exists | RED RC1, SHA `6ac9310187ad63cf6e83b41b2e2620a3e0832d14bce8a464b4e220e5fb485944` |
| Initial incorrect directory-create assumption | RED RC1, SHA `35b36400f17815ad221cf1ef6085363c4efeaa2af4197bf42dbca21a28431151` |
| Final Windows fixture matrix on SHA `12f1db3` | **7 main + 6 nested PASS, 0 SKIP, RC0**, SHA `cccdbc0a4306429359dd332fbcf297c43e299d13faa4777c4cb67f404ab464f5` |
| Native new/close/replay and mid-read negatives, final SHA | **4 main + 3 nested PASS, RC0**, SHA `2588c13243224d66f1d65e24f8c46d4e6392a845f5464c7dff169ddece0987ed` |
| Atomic N-ID CAS, final SHA | **4 main + 11 nested PASS, RC0**, SHA `9c2d9174013f0f707a76fecc2f092485a49e4d4931872e77d6c0a3af45215eb7` |
| Go vet, build | RC0 / RC0; source-only binary SHA `2d66678da266d0f2ec9e9cb053daf9bcf0824e4588007f42037d7e7cf8432b70`. **Not installed**. |
| Independent `gpt-6-sol medium` on frozen SHA | **PASS, P0/P1/P2 = 0**; result SHA `2f9ebe1d9dc3b968134f63f3088b191ea57b908f99f45c7e5195b06c5be0a316`; reviewed source, not host Go-log reproduction |

Final QA/semantic evidence stored only under `R:/-4-/air-worker/work/N124_WORKER_A_20261018`, with exact SHA in the [machine receipt](receipts/AIRWORKER_N124_A3_2_WINDOWS_HANDLE_FIXTURE_20261018.json). No private logs or credentials included in public source.

## Open and blocked

N-124 **OPEN** until single canonical GitHub owner/ref/signer, installed version+binary attestation, trustworthy durable cross-process generation CAS, full Go QA, exact source-bound independent judgment and native core closure. A3-1 GitHub atomic custom-ref protocol capability was proven earlier under a separate LPR yes and test refs cleaned; **that did not grant canonical PLAN publisher rights**. DEV-136 `-apply` remains blocked; AirLegal N-111 OPEN. Neighbor worker B/N-093 and four Hermes WIP preserved. Installed `0.11.8` SHA `1b4008af05110214934d46c0ab8c5236341cd1c699f54efb895c2f9f64a94a94` unchanged.

**Rollback:** discard/revert the three feature source commits only; no production action to roll back.

**Developer documents:** [Git atomic push](https://git-scm.com/docs/git-push); [GitHub Git refs API](https://docs.github.com/en/rest/git/refs). Further work via native AirWorker core only after owner/authority gate.
