---
name: analyze-legal-doc
description: Verification-first review of a legal document — extract text locally, decompose into checkable theses with character-offset spans, verify every legal qualification against the primary source as a three-slot syllogism, and emit knowledge cards marked verified, candidate or unverifiable. Use when reviewing incoming legal work-product (analysis, memo, deck, opinion, contract clause set) before accepting any of it into a knowledge base, when checking whether someone's legal reasoning actually holds, or when deciding which part of foreign material is safe to reuse. Do NOT use for drafting documents or for summarizing a file the user only wants read.
---

# Analyze legal document

A well-argued legal text is persuasive whether or not it is correct. Reading it and
agreeing is not review. This skill checks it.

Measured context: leading commercial legal-research tools hallucinate on 17–33% of
queries even with retrieval over proprietary primary sources. **Retrieval is not
verification.** Verification is a separate, explicit, preferably mechanical step — that
is what this skill is.

## Governing rules

1. **Analysis precedes slicing.** Never decompose foreign material into an accepted
   knowledge layer by default. Verify first; only the confirmed part is written, and
   only after the decision-maker approves.
2. **Never assert a legal position from model memory.** Every norm is read from a primary
   source in the current session and recorded with what it was checked against.
3. **Verify even what looks obviously right.** Checking the obvious is where the largest
   gains come from — a thesis argued informally often turns out to rest on a rule the
   author never cited. Hunt for that as deliberately as for error.

## Start

1. Load the private profile config: `LEGAL_ANALYSIS_CONFIG` env var, or the `config_path`
   user setting. Without it, run in generic mode and **ask** where results should go.
2. Initialize the learning store and pull what is already known — this is what stops the
   skill re-verifying the same article every session:

   ```bash
   python scripts/learning_store.py init
   python scripts/learning_store.py export-context
   ```

   `export-context` returns active rules **and** the base of already-verified norms.
   Check it before searching for any norm.
3. Identify the document, its author and contour. If the project is not obvious, check
   the project's own contact and registry records **before** asking — a known author is
   usually already on file. Never infer a project from the sender address alone.
4. Read the file's real bytes. Never review a document you have not opened.

## Extract

Extract text **locally** — never upload the document to a cloud converter. Web access is
for reading published legal sources only: public text comes in, the client's document
never goes out.

- clean first (HTML, PDF artifacts, hyphenation, whitespace), **then** take offsets;
- write the cleaned text to a companion `.txt` beside the original and record its hash —
  spans point at that file, not at the binary;
- verify the yield: a 13-slide deck returning 200 characters means extraction failed,
  not that the deck is empty;
- hash the original (SHA-256), keep it outside version control.

Read `references/extraction.md` for formats, failure modes and encoding traps.

## Decompose

Break the material into **checkable units**, not into a summary. Every unit is one of:

- **factual claim** — what the document says. Requires a character-offset span.
- **legal qualification** — a three-slot syllogism: norm → fact → conclusion.
- **methodological claim** — how the author says work should be done.
- **opinion** — argued, not verifiable. Marked as such and never promoted.

A qualification written as a single sentence cannot be checked slot by slot, and review
of it collapses into agreement. Decomposition is mandatory, not stylistic.

Read `references/thesis-model.md` — it defines spans, the syllogism and the outcome set.

## Verify — the core of the skill

For each slot, per its own mechanism:

```text
норма            → primary source, read in this session
факт             → slice the file at the span and compare (no model involved)
вывод            → logical validity given the two premises
```

Record one of five outcomes: `confirmed` · `defect` · `reinforced` · `unresolved` ·
`unverifiable`.

**`defect` and `unverifiable` are not the same thing.** A source that does not cover a
question is not evidence that a citation was invented. `unverifiable` requires a
`coverage_note` explaining why the check is impossible.

Persist every check so it is not repeated:

```bash
python scripts/learning_store.py record-norm --input norm.json
python scripts/learning_store.py lookup-norm --act "ГК РФ" --article "431.2"
```

`unresolved` is recorded too — a fruitless search, written down, is a result.

**Watch for the new factual question.** When verification produces `reinforced`, it often
implies a fact nobody established — e.g. a rule that turns on *who drafted the clause*
when no material says who drafted it. That question is a fork, not a footnote: surface it
explicitly.

Read `references/verification.md` for source hierarchy, precision-over-force and the
two recurring error shapes.

## Review

Four parts, in this order:

1. **What holds** — genuine strengths, stated plainly. A review opening with criticism
   is discounted by author and reader alike.
2. **Defects**, ordered by significance, each with the source that defeats it.
3. **Applicability** — is the material inside the contour it was produced for? A
   technically excellent document aimed at the wrong problem is still misaimed.
4. **What is reusable** — the layer that survives independently of the case.

Be accurate in both directions: if the material is strong, say so even when the requester
expected it to be weak.

**Compare against the author's earlier work when available.** A defect present in a late
document but absent in an earlier one is a *regression*, not a competence gap — and the
correction to give differs completely: "return to your own standard" rather than "learn
this". Getting this backwards damages the relationship with a capable partner.

## Emit cards

Only after the decision-maker approves the boundary.

- **method cards** — reusable instruments, independent of the case;
- **legal knowledge cards** — one verified norm per card, `verified_against` in front matter;
- **candidate cards** — unverified, with a visible banner in the body and an explicit
  list of what to check;
- **case layer** — normally NOT emitted; say so explicitly rather than dropping it silently.

The model produces `candidate` only. Promotion to verified requires a machine check
against the source or explicit acceptance — per single qualification, never per document.

Link every card back to the source and the review, and connect the new layer to
pre-existing related content. An island in the graph is an unfinished job.

Read `references/output-cards.md` for card shape and marking rules.

## Learn

After the decision-maker corrects anything, record it — this is what makes the next run
better rather than identical:

```bash
python scripts/learning_store.py record-feedback --input feedback.json
```

Rules stated directly by the decision-maker are added with `add-rule`: they are active
immediately, marked `origin=human` and locked, and the machine may propose changes to
them but never apply them.

## Report and stop

Report what was verified and against what, what was rejected and why, where the boundary
was drawn, and what remains open. State plainly what was NOT done.

Do not write into an accepted knowledge layer, send anything outward, or create tasks
without a separate explicit instruction.
