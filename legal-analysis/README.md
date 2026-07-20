# legal-analysis

Verification-first review of legal documents.

The skill exists because of a specific failure mode: a well-written legal analysis
is persuasive whether or not it is correct. Reading it and agreeing is not review.
This plugin forces every legal qualification in a document to be checked against the
primary source before any part of it is accepted into a knowledge base.

## What it does

```text
document (pptx / docx / pdf / scan)
  → local text extraction (no cloud conversion of the document itself)
  → decomposition into checkable theses and legal qualifications
  → verification of each qualification against statute + supreme-court guidance
  → critical review: what holds, what is defective, what is missing
  → knowledge cards, each marked verified | candidate
  → report to the decision-maker; nothing enters the accepted layer without approval
```

## Two findings it is designed to produce

Verification is not only a filter. In practice it produces both:

- **Defects** — a thesis that looks sound but fails against the source. Example from
  the run this skill was built on: two legal constructions presented as equivalent
  alternatives, where binding guidance makes one of them fall away by default.
- **Reinforcement** — a thesis the author argued informally that turns out to rest on
  a direct statutory rule the author never cited. The same check that kills bad
  reasoning upgrades good reasoning.

Both are reported. A review that only produces criticism is doing half the work.

## Hard rules

1. **Never assert a legal position from model memory.** Every norm is read from the
   primary source in the current session and recorded with what it was checked against.
2. **Established fact and hypothesis are marked separately, in place.** A disclaimer at
   the end of a document does not license categorical claims in its body.
3. **Unverified material is not silently upgraded.** It is emitted as `candidate` with
   an explicit list of what still needs checking.
4. **Analysis precedes slicing.** Foreign material is never decomposed into the
   accepted knowledge layer by default — only its verified part is, after approval.

## Privacy split

This repository is public. It carries the **method** only.

Vault paths, target projects, house quality standards and the accumulated base of
verified norms live in a private config outside this repo, referenced through the
`config_path` user setting. Without that config the skill runs generically and asks
where to put results.

Document text is extracted **locally**. The document itself is never uploaded for
conversion. Web access is used only to read published legal sources — statutes and
court guidance — never to send the material under review.

## Status

v0.1.0 — derived from a real review run (13-slide legal deck, 2026-07-20) that
produced four defects, one reinforcement and eleven knowledge cards. Not yet exercised
across multiple document types.
