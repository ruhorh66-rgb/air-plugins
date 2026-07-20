# Thesis model

How a claim is represented so that it can be checked mechanically rather than judged.

## The problem this solves

A legal analysis is persuasive whether or not it is correct, and a monolithic claim
cannot be checked point by point — only agreed or disagreed with. Splitting a claim into
independently verifiable slots is what turns review into verification.

Reference points: Stanford RegLab measured hallucination rates of 17% (Lexis+ AI), 33%
(Westlaw AI-Assisted Research) and 43% (bare GPT-4) on legal research queries. Retrieval
over a proprietary corpus of primary sources roughly halves the rate and does not remove
it. **Retrieval is not verification.** Verification has to be a separate, explicit,
preferably mechanical step.

## 1. Every factual claim carries a character offset, not a quotation

```json
{
  "span": {"file": "05_ORIGINALS/PPTX/FILE-031-00060_....pptx.txt",
           "start": 4172, "end": 4310},
  "quote_cached": "…"
}
```

The offset is canonical; the cached quote is a convenience copy. Correctness is checked
by slicing the file and comparing — **no model participates in that check**. A paraphrase
that drifted from the source fails the comparison immediately.

Rules:

- a claim about what the document says has a span, or it does not enter a card;
- spans point at the **extracted text file**, kept alongside the original, not at the
  binary — offsets into a PDF or PPTX are meaningless;
- cleaning (de-hyphenation, whitespace, artifact removal) happens **before** offsets are
  taken, and the cleaned text is what gets stored;
- if extraction is re-run and the text changes, existing offsets are invalid — record
  the extraction hash with the spans.

This is the cheapest anti-hallucination control available: it costs one file read and
catches every silent rewording.

## 2. Every legal qualification is a syllogism, not a sentence

Three slots, three different verification mechanisms:

```text
major premise   норма            → verified against the primary source
minor premise   факт из документа → verified against a span (rule 1)
conclusion      правовой вывод    → checked for logical validity
```

Example, decomposed:

```json
{
  "major": {"norm": "ст. 431.2 ч. 4 ГК РФ",
            "status": "confirmed",
            "verified_against": "КонсультантПлюс, сверено 2026-07-20"},
  "minor": {"claim": "договор является договором об отчуждении долей",
            "span": {"file": "...", "start": 812, "end": 968},
            "status": "confirmed"},
  "conclusion": {"text": "ответственность наступает независимо от знания заверителя",
                 "status": "valid"}
}
```

A claim written as one sentence — «по 431.2 продавец отвечает» — cannot be checked
slot by slot, and review of it degenerates into agreement. This decomposition is the
single largest change from the skill's first version.

Practical consequence: **a syllogism can fail in three different ways**, and the failures
have different remedies. Wrong norm → re-verify. Unsupported minor premise → find the
span or drop the claim. Invalid inference → the author's reasoning is broken even though
both premises hold.

## 3. Verification has five outcomes, not two

| Outcome | Meaning | What it licenses |
|---|---|---|
| `confirmed` | source supports the claim as stated | emit as verified knowledge |
| `defect` | source contradicts it, or it is materially imprecise | report with the defeating source |
| `reinforced` | supported, and resting on a rule the author never cited | emit, and tell the author |
| `unresolved` | searched, nothing found | record the search as a result |
| `unverifiable` | the source cannot answer this in principle | record with `coverage_note` |

The distinction that matters most is `defect` vs `unverifiable`. A source not covering a
question is **not** evidence that a citation was invented. Collapsing the two manufactures
accusations of hallucination — the failure mode CourtListener's maintainers warn about
explicitly: a 404 in the database does not mean the citation is fake, because the database
does not index everything.

`unverifiable` requires a `coverage_note` saying *why* it cannot be checked (outside the
source's scope, paywalled, unpublished, pinpoint citation to proprietary pagination).
Without that note the status is indistinguishable from laziness.

`unresolved` is likewise a result, not a failure: recorded, it stops the next session
repeating a search that already came back empty.

## 4. Extraction is deterministic before it is generative

Dates, sums, deadlines, case numbers, party names and statutory references are pulled by
rules, not paraphrased by a model. This removes an entire class of hallucination — the
misquoted number — at no cost.

Legal-aware sentence segmentation matters: a naive splitter breaks «ст.», «п.», «ч.»,
«№», «г.» and cuts citations in half, after which every downstream check operates on
fragments.

## 5. Only a human or a deterministic check produces `verified`

The model produces `candidate` and nothing else. Promotion requires either a machine
comparison against the source or explicit acceptance by the decision-maker.

Granularity of acceptance is **one qualification**, not one document. A document is never
"approved" as a whole — that is how an unchecked claim rides in alongside twenty checked
ones.
