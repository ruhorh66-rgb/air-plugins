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

Vault paths, target projects and house quality standards live in a private config
**outside any git working tree**, referenced through the `config_path` user setting or
the `LEGAL_ANALYSIS_CONFIG` environment variable. Without that config the skill runs
generically and asks where to put results.

Keeping it outside a working tree is deliberate, not tidiness. A config that a running
tool reads by absolute path, stored inside a repository, is deleted from disk by any
branch switch — the tool then runs unconfigured with no error pointing at the cause.
Store runtime configuration in a runtime location.

The accumulated base of verified norms is not in the config at all: it lives in the
skill's local learning database (see below), which is likewise outside git.

Document text is extracted **locally**. The document itself is never uploaded for
conversion. Web access is used only to read published legal sources — statutes and
court guidance — never to send the material under review.

## Learning store

The skill keeps a local SQLite database of what it proposed, what the decision-maker
corrected, and the rules derived from those corrections — plus an accumulating base of
norms already checked against a primary source, so the same article is not re-verified
from scratch every session and fruitless searches are not repeated.

```
python scripts/learning_store.py init
python scripts/learning_store.py lookup-norm --act "ГК РФ" --article "431.2"
python scripts/learning_store.py record-norm --input norm.json
python scripts/learning_store.py export-context
```

An `unresolved` verification is recorded as a result, not discarded — that is what stops
the next session repeating a search that already came back empty.

The database implements a shared cross-skill contract so one analyzer can read every
skill's store in a single pass: a `learning_meta` passport, `feedback` (raw corrections)
and `patterns` (derived rules with confirmation and counterexample counters). It never
stores document text, extracted bodies, attachment bytes or credentials — external
objects are referenced only through an HMAC of their locator.

## Status

**v1.0.0** — 2026-07-20.

The jump from 0.1.0 is a MAJOR bump, not inflation: the thesis model changed in a way
that breaks backward compatibility. A factual claim now requires a character-offset span
to be promoted at all, a legal qualification is stored as a three-slot syllogism rather
than a sentence, and verification returns one of five outcomes instead of two. Artifacts
produced by 0.1.0 do not satisfy the new model — they are not upgradeable in place, only
re-derivable. Under semantic versioning that is MAJOR by definition.

Derived from two real review runs: a 13-slide legal deck and four recovered documents by
the same author, together producing four defects, three reinforcements, twenty-two
knowledge cards and two decision forks. The reinforcement cases shaped the method —
verification is treated as an amplifier, not only a filter.

Not yet exercised on: contracts, court decisions, scanned originals requiring OCR, or
any document outside a single author's work-product. The learning store is live but
holds one rule and no episodes — the skill has not yet been corrected by anyone.
