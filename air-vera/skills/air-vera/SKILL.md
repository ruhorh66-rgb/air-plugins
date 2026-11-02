---
name: air-vera
description: Use the installed native AirVera CLI and accepted product documents; this skill does not grant authority or duplicate policy.
---

# AirVera — native host reference

Read `PRODUCT_INSTRUCTION.md` first, then accepted `config/skill_registry.json`. Operational recipes live in `OPERATIONS.md`; accepted product files, not this skill or chat memory, define capabilities and permissions.
`air-vera.exe` is the sole control plane. No `automation_controller.py` or host-policy fallback.

Read-only checks: `air-vera health --json`, `air-vera status --json`, `air-vera list --json`; simulation only: `air-vera cycle --dry-run --json`.

For native handoff, declare the actual host/session/model; only that same identity completes. Open: `air-vera handoff next --host chatgpt --session <session-id> --model <actual-model> --json`.
Complete: `air-vera handoff complete --id <id> --host chatgpt --session <session-id> --model <actual-model> --result-file <path> --json`.
Host lifecycle uses `air-vera hook <event>` and records only metadata, never raw host payload.

## Domain routing and safety
- Read the skill registry on startup and load domain skills only on trigger. Finance, budgets, БДР/БДДС (BDR/BDDS), payment calendars, accounting and related mail route to `air-finance-vera` at its exact registered path.
- User-taught procedures update canonical development skills and reach runtime only through an accepted immutable release; a skill cannot grant authority.
- Use the **owner product** native CLI: `air-vera mail`, `air-vera files`, `air-vera calendar`, `air-vera contacts`. Check installed version/resident/help; choose the **canonical built-in** mechanism, never another **wrapper**.
- **No fallback** to AirComms, rclone, direct Yandex, SMTP, IMAP, WebDAV or a **consumer adapter**. On **owner failure**, record **evidence**, open a **backlog** item and **stop dependent** steps.
- For `mail approve`, `mail confirm`, `files approve` and future `calendar approve` / `contacts approve`, use only commands actually available in the **installed** native CLI and accepted `PRODUCT_INSTRUCTION.md` / `OPERATIONS.md`, with separate LPR/OPERATOR grants. Both calendar and CardDAV contact issuers are source-only in 0.2.9 until separately approved and installed. Contact update/merge require exact ETag; delete requires a distinct LPR gate. Never hand-build AdapterEnvelope, invoke internal `protocol-grant`, call direct DAV fallback or reveal secrets.
- For an **ambiguous** UNKNOWN/timeout/`reconcile_required`, inspect the native **receipt** and **provider state**; **reconcile before retry**. Never assume no effect.
- Publish/install, production cutover, destructive operations, reboot and stopping foreign services require separate explicit LPR. For 0.2.8+ use native `release-status` and its accepted journal, not raw `accepted.json`; never hot-edit.

## Operator fast path and preserved phase lessons (08–09.10.2026)

Source: plan branch fd88d22, Core sections Fast path, One mail touch, SESSION-LEARN-20261009-DOCX-WIKI-MAIL-FASTPATH and SESSION-LEARN-20261009-ONE-REPLY-ARCHIVE-7M14. These are source-only procedures, NOT new native commands or permissions.

1. **Reuse / freeze.** When content and addresses are approved, reuse the exact source, verified recipients and references. Produce one final DOCX and SHA-256; check names, dates, page count and UTF-8. No unbounded repeated source searches, format revisions or repeated CLI help inspection. Missing signatory authority remains a question for LPR.
2. **Hard mail preflight.** Before compose/send verify exact sender, To, required CC/self-CC, subject, UTF-8 round-trip and attachment bytes/SHA. Every outgoing business message drafted by the agent requires the full verified corporate signature (name, position, organization and approved contact details) and the exact text **«Письмо подготовлено с помощью ИИ-агента.»** in the outgoing body. No exact signature or disclosure = STOP. Never invent personal details. Any change invalidates the old payload digest, target and grant.
3. **One SMTP intent.** Use one public native route supported by the installed Vera version: compose -> exact authorized approve -> send once, OR a supported native reply path with required authority; not both for the same message. Record Message-ID, payload SHA, idempotency and receipt before mutation. UNKNOWN / RECONCILE_REQUIRED means bounded native read-only verification of an exact self-copy or Sent record, no automatic retry or second transport. A self-copy does not establish external delivery.
4. **Bounded exact mail move.** When the LPR has approved archive of a known UID/Message-ID, use only installed native air-vera mail move/archive and the required distinct confirmation grant. Check destination readback. Historical success of mail.move --destination Archive in 0.2.5 does not prove live acceptance of another verb on 0.2.8. Never construct internal adapter envelopes.
5. **One document / one verified Wiki batch.** For approved storage use air-vera files with its exact write grant and one provider SHA readback; do not turn a file receipt into mail authority. Preassemble source/document/decision/action/outgoing cards and write them via public AirWiki CardBatch with verified_by_read. AirStorage indexing is derived; run Graphify at most once after the accepted batch. Keep customer/source IDs stable, separate internal sending, external delivery and external acceptance.
6. **Recover and measure.** On an interrupted chat resume by the original receipt/operation ID, not a second SMTP/DAV mutation. Measure wall-clock, actual native/provider durations, host calls, number of sends and Wiki batches. Historical 7m14s and targets of <=6 calls were operational lessons, NOT evidence that the current target was met.

At phase changes record a reusable lesson through a proven installed native learning API, otherwise as a source proposal/receipt without pretending a new installed feature. The CalDAV/CardDAV 0.2.9 source-only issuers do not exist in accepted installed 0.2.8 and do not authorize a real provider effect.
