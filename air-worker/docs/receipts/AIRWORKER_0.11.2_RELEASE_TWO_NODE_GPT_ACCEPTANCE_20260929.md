# AirWorker 0.11.2 release + two-node GPT acceptance — 2026-09-29

Release identity:
- accepted package commit: `d33424d089fd8576980be5eb09af9eb4d36a1751`
- embedded clean source: `b3cce88bd9cdb5e19655512755fad61bee1ec0a8`
- tag/release: `air-worker--v0.11.2` / `AirWorker 0.11.2`
- rollback: `air-worker--v0.11.1`
- CLI SHA-256: `CC12F82CF79A6EB183D065D0EE604DD9585DC5B9070B172FEDE44A13534ACEE5`
- tray SHA-256: `CF68DB8C49922D0F92189861D69C4A1F470768B69E39E2007EE601ECEA3E860A`
- source zip SHA-256: `FD8E553EA56A6BE455E5230537563DD6C945453D2D39946B2DD891CA150F2FA5`

Package acceptance:
- clean build: `vcs.modified=false`, exact embedded source on CLI/tray;
- plugin PASS; Hermes 8/0; binary/drift PASS; boundary PASS; shell PASS; reproducibility 43/0; selftest 36/0;
- GitHub release asset digests match the accepted bundle.

SRVLM01 live:
- Claude/Codex canonical GitHub marketplace/cache 0.11.2;
- live installed from canonical Claude GitHub cache; version 0.11.2, revision `b3cce88...`, CLI SHA matches release;
- `selfcheck`: warnings=0, violations=0, not_proven=0; Claude/Codex payload SHA identical; tray running;
- installed-binary GPT smoke in disposable package copy: session principal=chatgpt PASS; plan node actor=gpt-window PASS; learn event schema/actor PASS; stale node visible; report selected plan-node source. Fixture/state removed.

AIR-ENV-002 live:
- Claude canonical GitHub marketplace/plugin 0.11.2;
- active AIR Commander Codex profile created/registered only through official Codex CLI, GitHub air-plugins + air-worker 0.11.2 PASS;
- live installed from this node's canonical Claude GitHub cache; version 0.11.2, revision `b3cce88...`, CLI SHA matches release;
- `selfcheck`: warnings=0, violations=0, not_proven=0; active Claude/Codex canonical GitHub 0.11.2 with identical payload SHA; tray running;
- same installed-binary GPT smoke PASS and disposable fixture/state removed.

No reboot, no UAC, no manual cache copying.

Scope note: this closes first-class GPT CLI/session/plan/learn/report operation and the AirWorker release. Automatic ChatGPT host hooks are separate live transport node N-004: AIR Commander source middleware is accepted, but production bridge deployment/cutover remains separately governed by that product's lifecycle plan.

AC7 proposal `LP-20260928T133444Z-31d4ca9c` remains PENDING_LPR; this release does not fabricate or consume its approval grant.
