# Verification

The part of the review that produces value. Everything else is preparation.

## Source hierarchy

Check in this order and stop at the highest available:

1. **Statutory text** — the article and paragraph itself, read in the current session.
2. **Binding higher-court guidance** — plenum resolutions, practice reviews, rulings of
   the supreme instance interpreting that norm.
3. **Settled lower-court practice** — usable as illustration, never as the sole basis.
4. **Commentary and secondary literature** — orientation only. A commentary is a lead
   to the primary source, not a substitute for reading it.

A position resting only on levels 3–4 is not verified. Mark it `candidate`.

## Where the text is actually read (RU contour)

The hierarchy above says *what* counts. This says *where* to get it, in order:

1. **Official publication** — `pravo.gov.ru` / `publication.pravo.gov.ru` for the act as
   published, and the court's own site (`vsrf.ru`, `arbitr.ru`, `kad.arbitr.ru`) for
   judicial acts. This is the only tier that is authoritative on wording.
2. **Reference legal systems** — КонсультантПлюс, ГАРАНТ: current consolidated text with
   edition markers, plus higher-court guidance. In practice the working tier.
3. **Other publishers of the statutory text** — sites that reproduce the code article by
   article. Usable to *read* a norm; not authoritative on the current edition.
4. **Practice aggregators** (sudact and similar) — for locating an act, then read it at
   its source.

Facts come from registers, never from the same page as the norm: ЕГРЮЛ, `kad.arbitr.ru`,
ФССП, ЕФРСБ. **Norm from the primary source, fact from the register** — a document
asserting both is one source making two different kinds of claim.

### When the source tier you need is unreachable

Network reality is part of verification, and it is not a detail to paper over. Measured on
one workstation on 2026-07-21: the two major reference legal systems, the official
publication portal and the case index all timed out, while general internet access was
fine; exactly one publisher of statutory text answered. Ten norms were read that session —
every one of them from a single tier-3 source.

That is workable for statutory text and **not** workable for practice: a site that
publishes codes does not publish plenum guidance, so a thesis needing level 2 of the
hierarchy stays `unresolved`, with the reason recorded.

Rules for that situation:

- record what you actually read, naming the publisher and the fact that it was the only
  reachable one — `verified_against` is what makes a re-check possible, so an inflated
  provenance is worse than a modest one;
- do not upgrade a tier-3 read into "checked against the official text";
- a thesis that needs higher-court guidance is `unresolved` with a `coverage_note`
  distinguishing *unreachable from here* from *does not exist* — the second is a claim
  about the law, the first is a claim about the network;
- when two publishers are reachable, read both and treat a discrepancy as a stop
  condition, not as an average.

Unreachability is an environment fault, not a licence to reason from memory. The rule
that a missing source blocks the analysis is unchanged by *why* it is missing.

### Diagnose the silence before accepting it

A source that "does not respond" is four different situations, and they call for four
different actions. Only the last one is a statement about the law:

| Symptom | Likely cause | Action |
|---|---|---|
| TCP connect fails or times out | Network path, DNS, or the host is gone | Environment problem; report it |
| **TCP connects, HTTPS times out** | Application-layer filtering of your egress | Route or exit differently — the source is fine |
| **HTTP 200, but the body is an empty shell** | The page renders its content with JavaScript; a raw HTTP client sees the skeleton | Wrong client, not a missing source. Open it in the browser panel and read it verbatim |
| Page loads *with its text*, article absent | The source genuinely does not cover it | `unverifiable` with a `coverage_note` |

The two middle rows are the ones that get misread as the last. In the run above, every
"dead" legal source completed a TCP handshake and then stalled on the request: the sites
were serving normally and declining a foreign exit point. One routing exception restored
all of them. Time spent recording `unresolved` verdicts was time spent documenting a
network setting.

The third row cost four norms. On 2026-07-22 items 43 and 45 of Plenum ruling № 49 stood
`unresolved` while the route to the source was proven and working: HTTPS returned 200 and
the body simply did not carry the text, because the page builds it client-side. Read
through the browser panel, all four closed as `confirmed` / `reinforced` the same session.

```text
get_page_text        → the readable text of the rendered page
javascript_tool      → query the DOM directly when the text needs locating
```

Panel boundary: `requestAnimationFrame` does not tick there, so canvas, video and
`read_page` are dead. Use `get_page_text` and `javascript_tool` and nothing else.

Only after the browser client has also failed does the source become unreadable — and then
record *what* specifically did not come back, not merely that it did not.

Check which of the four you have before writing the verdict down — an `unverifiable` that
was really a routing problem or a wrong client is a false statement about the law, filed
permanently.

## Quote whitelist: never generate a citation, only retrieve one

The strongest available defence against fabricated citations, and it is structural rather
than exhortative: **a verbatim quote may only be emitted if it already exists in the
verified-norm store.** The model does not produce the quote — it looks it up.

```bash
python scripts/learning_store.py lookup-norm --act "ГК РФ" --article "431.2"
```

Rules:

- a quote not present in the store is **not quoted**. State the norm in your own words
  and mark it `unresolved` until the text is read from a primary source and recorded;
- when a norm is verified, record it with `record-norm` **including the exact wording
  relied on**, so the next session retrieves rather than re-derives it;
- never assemble a quote from non-adjacent fragments — that is the standard mechanism by
  which a plausible fabrication is produced;
- an empty store is not a licence to improvise. If nothing is recorded and no primary
  source can be read in this session, the analysis is blocked, not guessed.

Asking a model to "be careful with citations" does not work. Removing its opportunity to
invent one does.

*Adopted from a partner's system specification reviewed 2026-07-20 (FILE-031-00066); its
whitelist design was stronger than this skill's original approach and is taken directly.*

### No exact norm — write close to it, but without the citation

The whitelist above governs the **quote**. This governs the **citation** — the article
number itself — and it is a decision-maker's rule, not a derived one: stated directly on
2026-07-22 while drafting a work-suspension letter ("pull the statute in if you can; if
not, write as close to it as possible").

- a citation is placed **only** for a norm read from the primary source in this session
  and checked for status. Otherwise there is no citation;
- where no exact norm fits the facts, phrase the demand in the logic of the legal
  construction — but with no article number and no quotation marks. **An approximate
  citation is worse than none**: it hands the other side a free argument and devalues the
  correct points beside it;
- never reach for a merely similar article to make a document look grounded. How
  persuasive the document looks is not a reason to cite;
- say out loud which points ended up with no normative basis. Silence about a gap reads
  as a covered point.

This is the drafting-side twin of the whitelist: the whitelist stops an invented quote,
this stops an invented ground.

## Fail-closed: absence blocks, it does not soften

When a required source is missing, the correct output is a **block with a stated gap**,
not a hedged answer:

```text
нет источника        → вывод заблокирован, перечислить чего не хватает
источник не покрывает → unverifiable + coverage_note
источник противоречит → defect + опровергающая ссылка
```

A confident answer assembled from an incomplete base is worse than no answer, because it
is indistinguishable from a complete one.

## What "verified" requires

All four, or the unit is not verified:

- the norm was **read in this session** from a source that publishes its text;
- the recorded claim matches what the text actually says, including its exceptions;
- **the article is still in force**, and the edition you read is identified — which
  amending law introduced it and from what date. A repealed article reads exactly like a
  live one;
- the recording captures **what it was checked against**, precisely enough to re-check.

The third condition is not ceremony. On 2026-07-22 art. 67 of the Russian fire-safety
technical regulation (123-ФЗ) — "Проезды и подъезды к зданиям, сооружениям и строениям" —
matched the facts of an outgoing letter word for word and was ready to be cited. It has
been **repealed**; the operative rule is item 1 part 1 of art. 90 of the same act. Nothing
in the text of art. 67 says so. Only its status does.

A citation to a dead norm damages the whole document: it hands the other side a free
argument and devalues the correct points standing next to it. When an article has been
repealed, look for the successor rule before concluding the point is unsupported.

Front matter of an emitted card:

```yaml
legal_norms_verified: true
verified_against: "<act>, <article/paragraph> (<source>), checked <YYYY-MM-DD>"
```

## Five outcomes

| Outcome | Meaning | Action |
|---|---|---|
| `confirmed` | Thesis holds as stated | Emit as verified knowledge |
| `defect` | Fails or is materially imprecise against the source | Report with the defeating source; do not emit as knowledge |
| `reinforced` | Holds, and rests on a rule the author did not cite | Emit, and tell the author what strengthened it |
| `unresolved` | No direct guidance located | Record as a result: "no direct guidance found"; keep as candidate |
| `unverifiable` | The source cannot answer this in principle | Record with a `coverage_note` saying why |

`unverifiable` without a `coverage_note` is rejected by the store rather than stored
half-filled — the note is the whole difference between "this cannot be checked" and
"nobody checked".

`unresolved` is a finding, not a failure. Recording it prevents the same fruitless
search being repeated later.

## Precision over force

The strongest version of a claim is the accurate one, not the categorical one.

Worked example from the run this skill was built on: the claim "a person without legal
education cannot represent another party in commercial court" is imprecise — the
procedural rule allows such a person to participate **alongside** a qualified
representative, but not to conduct the case or sign the pleading independently. The
accurate version is narrower *and* argues the point better: the ceiling is second chair
to someone else's lawyer.

Never sharpen a claim past what the source supports. An overstated position collapses
the moment the opponent reads the article.

## Hunting for reinforcement

Do not only test theses for failure. For each thesis the author argued informally, ask:
**is there a direct rule that says this?**

Worked example from the same run: the author argued "opportunity to inspect ≠ actual
knowledge" as pure reasoning. Checking it surfaced a statutory provision under which,
in business and share-transfer transactions, liability for inaccurate representations
arises regardless of the representing party's knowledge, with a presumption that the
other side relied on them. The thesis was not merely correct — it had direct backing the
author had missed, removing two standard defences.

Reinforcement is what makes the review worth more to the author than a grade.

## Cross-check the author's own framing

Two recurring error shapes worth testing explicitly:

- **False equivalence.** Alternatives presented as equally weighted when a rule makes
  one fall away by default. Test each branch separately against the source rather than
  accepting the author's framing of the choice.
- **Regime mixing.** Elements of one legal construction imported into another — e.g.
  demanding proof of causation belonging to a fault-based claim inside a construction
  that operates without fault. Verify which elements each regime actually requires.

## Boundary

Verification covers **legal qualification**. It does not establish disputed facts.

If the underlying primary material — the judgment, the contract, the register extract —
is not present in the contour, factual statuses stay unverified no matter how confident
the reviewed document sounds, and no matter how internally consistent it is.
