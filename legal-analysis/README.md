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
python scripts/test_learning_store.py     # self-check, no framework needed
```

An `unresolved` verification is recorded as a result, not discarded — that is what stops
the next session repeating a search that already came back empty.

The database implements a shared cross-skill contract so one analyzer can read every
skill's store in a single pass: a `learning_meta` passport, `feedback` (raw corrections)
and `patterns` (derived rules with confirmation and counterexample counters). It never
stores document text, extracted bodies, attachment bytes or credentials — external
objects are referenced only through an HMAC of their locator.

## Status

**v1.1.0** — 2026-07-21. MINOR: the thesis model and the verification outcomes are
unchanged, so v1.0.0 artifacts remain valid. What changed is coverage — an OCR branch for
scans, a stated retrieval order for reading Russian legal sources, and the corrections
that came out of running the skill on material it had never seen.

### v1.0.0 (2026-07-20) — where the model came from

The jump from 0.1.0 was a MAJOR bump, not inflation: a factual claim now requires a
character-offset span to be promoted at all, a legal qualification is stored as a
three-slot syllogism rather than a sentence, and verification returns one of five outcomes
instead of two. Artifacts produced by 0.1.0 are not upgradeable in place, only re-derivable.

It was derived from two review runs on one author's work-product — a 13-slide legal deck
and four recovered documents — producing four defects, three reinforcements, twenty-two
knowledge cards and two decision forks. The reinforcement cases shaped the method:
verification is treated as an amplifier, not only a filter.

### v1.1.0 — exercised on the document types it had never seen

Eight real documents across four project vaults, six distinct authors:

| Type | Document | Extraction |
|---|---|---|
| Court act | Определение об оставлении встречного иска без движения, АС СПб и ЛО | text layer, 3 724 chars |
| Contract | Договор подряда (проект), группа заказчика | text layer, 94 075 chars |
| Scanned original | Тот же договор, подписанный экземпляр, 36 стр., 14.9 MB | OCR fallback, 121 380 chars |
| Cross-author set | 4 определения трёх арбитражных судов + досудебная претензия контрагента | text layer |

Produced: **1 defect in the reviewed material, 2 reinforcements, 3 forks, 1 unresolved**
(recorded as a result, not discarded), plus **2 defects in this skill's own code and
documentation**, both fixed here:

- `record-norm` accepted a `coverage_note`, the table had the column, and the INSERT
  dropped it — so `unverifiable`, the outcome whose entire meaning is *why* the check is
  impossible, was stored without its reason. Now persisted, required, exported, and
  covered by `scripts/test_learning_store.py`.
- `references/verification.md` documented four outcomes where the model has five.

Extraction findings, measured against a ground-truth pair (the same contract as draft text
and as a signed scan — 99.4% shared distinct words, 10 of 12 sampled 15-word fragments
verbatim):

- 0.77% of OCR words came back rendered entirely in Latin homoglyphs (`MK` for `ГК`);
  normalising recovered a statutory reference the deterministic pass had lost;
- OCR **over**-extraction turned out to matter as much as under-extraction: 7 distinct
  percentage values in the text layer became 17 in the OCR, ten of them artefacts of
  stamps and tables.

The learning store is no longer empty of experience: **15 verified norms** (13 confirmed,
1 reinforced, 1 unresolved), **5 active patterns**, **2 recorded episodes**. Norms cover
АПК ст. 128 / 129 / 132 / ч. 4 ст. 270, ГПК ч. 4 ст. 330, КАС ч. 1 ст. 310 — the three
codes that make the "numbering does not transfer" trap concrete — and ГК ст. 395, 397,
407, 431.2, 855, НК ст. 76.

### What is still not exercised

Notarial and registry documents, handwritten inserts, foreign-language originals, and any
document outside the Russian procedural/contract contour. No episode yet originates from a
decision-maker's correction — both recorded episodes are self-corrections from measurement.

One environmental limit shaped these runs and is worth stating, because of how it was
found and what it cost: on the workstation they were made, the reference legal systems, the
official publication portal and the case index were all unreachable, so every norm was read
from a single tier-3 publisher of statutory text — recorded in each norm's
`verified_against` rather than smoothed over, and the reason the one thesis needing
higher-court guidance stayed `unresolved`.

It turned out not to be censorship or DNS. TCP connections to those hosts completed
through the tunnel while HTTPS requests timed out — the signature of application-layer
geo-filtering against a foreign egress, fixed by routing those destinations directly rather
than by anything the skill could do. Two norms were then re-read at a reference system and
their provenance raised.

The transferable part is the diagnostic: **when a legal source "does not respond", check
whether TCP completes before concluding the source is blocked or gone.** The three cases —
unreachable, geo-filtered, and genuinely absent — call for completely different actions, and
only the third is a statement about the law.
