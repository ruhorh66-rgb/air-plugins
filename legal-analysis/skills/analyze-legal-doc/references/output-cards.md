# Output cards

What gets written, how it is marked, and what deliberately does not get written.

## Four card types

| Type | Holds | Emitted when |
|---|---|---|
| `method` | Reusable instrument or standard, independent of the case | The instrument works without the case attached |
| `legal_knowledge` | One verified norm and its practical consequence | Verification returned `confirmed` or `reinforced` |
| `candidate` | Thesis that is plausible but unverified | Verification returned `unresolved`, or the source could not be read |
| `analysis` | The review itself — strengths, defects, boundary, open forks | Once per reviewed document |

One norm per legal card. A card covering three articles cannot be cited, superseded or
retired independently.

## Marking rules

Front matter carries the epistemic status. It is not optional and not implied by tone.

Verified:

```yaml
status: draft
review_status: needs_human_review
legal_norms_verified: true
verified_against: "<act>, <article> (<source>), checked <YYYY-MM-DD>"
confidence: high
```

Candidate — additionally opens with a visible banner in the body, because front matter
is invisible in most rendered views:

```yaml
status: candidate
review_status: needs_verification
legal_norms_verified: false
confidence: medium
```

```markdown
> **Status: not verified.** Assessed as professionally sound but not checked against
> the primary source. Not for external use until verified. To check: 1) … 2) … 3) …
```

A card without a status marking will be read as established fact by the next person,
including by a future model session. That is how an unverified claim becomes a
load-bearing assumption.

## Fact and hypothesis are marked in place

A qualification at the end of a document does not license categorical statements in its
body. If a table carries statuses like "proved" / "not proved" while the underlying
material was never checked, the marking belongs **in the table**, not in a closing note.

This applies to cards this skill emits and is also a defect to look for in material
under review.

## What is normally not emitted

**The case layer.** Case-specific facts, amounts, figures and party-specific findings
drawn from unverified material do not enter an accepted knowledge layer. They inherit as
fact and are near-impossible to retract once cited.

Do not drop this silently. State in the review and in the hub card that the case layer
was deliberately not carried over, and why. A month later the absence will otherwise read
as an oversight.

## Linking

Every card links back to:

- the source document card;
- the analysis card that validated it;
- the author or origin, where a contact record exists.

Then connect the new layer to **pre-existing related content** — earlier cards, related
projects, the strategy or scope record it constrains. Verify link targets resolve; an
unresolved wiki link is a silent break.

A new layer that links only to itself is an island, and an island is an unfinished job.

## Hub card

More than three cards from one intake get a hub card that carries the map of the layer
and, explicitly, **the boundary** — what was taken, what was refused, and the rule the
boundary was drawn by. The hub is what makes the layer navigable later.

## Registration

If the target vault has a registry, register every emitted card. An unregistered card is
invisible to anything that reads the registry rather than the folder.
