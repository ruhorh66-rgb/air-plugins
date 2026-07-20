---
name: analyze-legal-doc
description: Verification-first review of a legal document — extract text locally, decompose into checkable theses, verify every legal qualification against the primary source (statute + supreme-court guidance), separate established fact from hypothesis, and emit knowledge cards marked verified or candidate. Use when reviewing incoming legal work-product (analysis, memo, deck, opinion, contract clause set) before accepting any of it into a knowledge base, when checking whether someone's legal reasoning actually holds, or when deciding which part of foreign material is safe to reuse. Do NOT use for drafting documents or for summarizing a file the user only wants read.
---

# Analyze legal document

A well-argued legal text is persuasive whether or not it is correct. Reading it and
agreeing is not review. This skill checks it.

## Governing rule

**Analysis precedes slicing.** Never decompose foreign material into an accepted
knowledge layer by default. Verify first; only the confirmed part is written, and only
after the decision-maker approves.

## Start

1. Load the private profile config if `config_path` is set (or `LEGAL_ANALYSIS_CONFIG`).
   It carries vault paths, target projects and house standards. Without it, run in
   generic mode and ask where results should go — do not guess a location.
2. Identify the document, its author, and which contour it belongs to. If the project
   is not obvious, check the project's own contact/registry records **before** asking
   the user — a known author is usually already on file. Do not infer a project from
   the sender address alone.
3. Confirm the document is reachable and read its real bytes. Never review a file you
   have not opened.

## Extract

Extract text **locally**. Do not upload the document to a cloud converter.

- Office/PDF with a text layer → local markitdown-class converter.
- Scans / images without a text layer → local OCR fallback.
- Record the extraction method and character count; a suspiciously small yield means
  the extraction failed, not that the document is empty.
- Hash the original (SHA-256) and store it outside version control unless the target
  vault explicitly permits binaries.

Read `references/extraction.md` for format handling and failure modes.

## Decompose

Break the material into **checkable units**, not into a summary:

- legal qualifications (which construction is being applied, and to what);
- factual assertions with a status claim (proved / not proved / established);
- methodological claims (how the author says work should be done);
- conclusions and what they rest on.

Each unit gets a provisional label: `verifiable_by_source`, `factual_needs_primary`,
`methodological`, or `opinion`.

## Verify — the core of the skill

For every `verifiable_by_source` unit:

1. Read the norm **from the primary source in this session**. Statutory text plus any
   binding higher-court guidance on that norm.
2. Record what it was checked against — act, article, paragraph, source, date checked.
3. State the outcome as one of:
   - **confirmed** — the thesis holds as stated;
   - **defect** — the thesis fails or is materially imprecise against the source;
   - **reinforced** — the thesis holds *and* rests on a rule the author did not cite;
   - **unresolved** — no direct guidance found (this is a result, record it as such).

**Never state a legal position from model memory.** If a norm cannot be read in this
session, the unit stays `candidate` — it does not get promoted on plausibility.

Look for reinforcement as deliberately as for error. An informally argued thesis that
turns out to have direct statutory backing is the highest-value output of a review:
it upgrades the author's work instead of only grading it.

For `factual_needs_primary` units: if the underlying primary material (the decision,
the contract, the register extract) is not in the contour, the status stays unverified
regardless of how confident the document sounds.

Read `references/verification.md` for source hierarchy and recording format.

## Review

Produce a structured review with four parts, in this order:

1. **What holds** — genuine strengths, stated plainly. A review that opens with
   criticism will be discounted by the author and by the reader.
2. **Defects**, ordered by significance, each with the source that defeats it.
3. **Applicability** — is this material inside the contour it was produced for? A
   technically excellent document aimed at the wrong problem is still misaimed.
4. **What is reusable** — the separable layer that survives independently of the case.

Be accurate about quality in both directions. If the material is strong, say so even
when the requester expected it to be weak; if it is weak, say so without softening.

## Emit cards

Only after the decision-maker approves the boundary:

- **Method cards** — reusable instruments and standards, independent of the case.
- **Legal knowledge cards** — one verified norm per card, with `verified_against`
  recorded in front matter.
- **Candidate cards** — unverified theses, marked `candidate` / `needs_verification`,
  carrying an explicit list of what to check, and flagged not-for-external-use.
- **Case layer** — normally NOT emitted: case-specific facts from unverified material
  do not belong in an accepted layer. Say so explicitly rather than silently dropping it.

Link every card back to the source document and to the review, and connect the new
layer to pre-existing related content — a new island in the graph is an incomplete job.

Read `references/output-cards.md` for card shape and the marking rules.

## Report and stop

Report: what was verified and against what, what was rejected and why, where the
boundary was drawn, and what remains open. State plainly what was NOT done.

Do not write into an accepted knowledge layer, send anything outward, or create tasks
without a separate explicit instruction.
