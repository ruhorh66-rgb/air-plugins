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

## What "verified" requires

All three, or the unit is not verified:

- the norm was **read in this session** from a source that publishes its text;
- the recorded claim matches what the text actually says, including its exceptions;
- the recording captures **what it was checked against**, precisely enough to re-check.

Front matter of an emitted card:

```yaml
legal_norms_verified: true
verified_against: "<act>, <article/paragraph> (<source>), checked <YYYY-MM-DD>"
```

## Four outcomes

| Outcome | Meaning | Action |
|---|---|---|
| `confirmed` | Thesis holds as stated | Emit as verified knowledge |
| `defect` | Fails or is materially imprecise against the source | Report with the defeating source; do not emit as knowledge |
| `reinforced` | Holds, and rests on a rule the author did not cite | Emit, and tell the author what strengthened it |
| `unresolved` | No direct guidance located | Record as a result: "no direct guidance found"; keep as candidate |

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
