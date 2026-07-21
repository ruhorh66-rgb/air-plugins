# Taste / craft / audit layers

The vendored UI/UX Pro Max skills give site-builder a **rules database** (styles,
palettes, font pairings, UX guidelines, a few GSAP presets). They answer "what is
correct." They do not, on their own, carry an opinionated **design direction**, a
deep **animation craft** layer, or a deterministic **quality audit**. Three widely-used
external Claude Code skills fill exactly those gaps. They are **complementary, not
replacements** — UI/UX Pro Max stays the base and these layer on top.

## The three skills

| Skill | Repo | License | Layer it adds | Plugs into | Status here |
|---|---|---|---|---|---|
| **Taste Skill** | `Leonxlnx/taste-skill` (tasteskill.dev) | **MIT** (© 2026 Leonxlnx) | "anti-slop" design *direction*: reads the brief, infers a design language, tunes VARIANCE / MOTION / DENSITY dials, ships GSAP code skeletons + a redesign-audit protocol | Phase 1 (design direction) | **vendored** → `skills/design-taste-frontend/` |
| **Emil animations** | `emilkowalski/skills` | **MIT** (© 2026 Emil Kowalski) | **animation craft**: enforces <300ms UI motion, custom easing, "when NOT to animate" | Phase 3 (animation), Phase 4 (motion audit) | **vendored** → `skills/emil-design-eng/`, `skills/review-animations/`, `skills/improve-animations/` |
| **Impeccable** | `pbakaus/impeccable` (impeccable.style) | **Apache-2.0** | deterministic design **quality audit + polish**: `/impeccable init\|audit\|polish\|…`, a shared `.impeccable/design.json` spec and a Live Mode | Phase 4 (review / polish gate) | **not vendored — external, optional** |

### Why each is a complement, not a duplicate
- **Taste Skill** decides *direction* (how bold, how dense, how much motion) for this
  specific brief. UI/UX Pro Max validates a direction against rules; Taste Skill picks
  one with intent. Use Taste Skill to set the dials, UI/UX Pro Max to keep it accessible.
- **Impeccable** is a *checker*, not a generator — it audits the built result against
  deterministic design rules and polishes. It runs after the site exists, so it never
  competes with the Phase 1–3 generators.
- **Emil animations** is *depth* on the one axis both databases treat shallowly (UI/UX
  Pro Max has ~16 GSAP presets; Framer Motion is the runtime). It refines the motion
  we already add, and audits it.

## Do not stack three design languages blindly
Taste Skill and Impeccable both carry an opinionated aesthetic. Running both as
*generators* on the same build fights itself. Safe division of labour:
- **Taste Skill** → sets direction and builds (generator).
- **Impeccable** → audits/polishes the result (checker) — do not also let it re-impose a
  second design language; use its `audit`/`polish`, not a full re-theme.
- **Emil animations** → owns motion only.

Pick **one** direction-setter (Taste Skill, or the vendored UI/UX Pro Max style pick),
not both at full strength.

## What is vendored, and why Impeccable is not

Taste Skill and the three Emil skills are each a **single self-contained `SKILL.md`**
(20–88 KB, no scripts, no path assumptions). They vendor cleanly per the `VENDOR.md`
policy: pinned copy, works offline months later, no installer step. Pinned commits and
provenance are in `VENDOR.md`.

Impeccable is deliberately **not** vendored, for three concrete reasons found by reading
the upstream repo (2026-07-20):

1. **Size** — 2.5 MB of reference material and scripts, versus 20–88 KB for the others.
2. **Hard-coded harness paths** — its `SKILL.md` runs `node .claude/skills/impeccable/scripts/context.mjs`
   as a mandatory setup step, and its `allowed-tools` is scoped to `Bash(node .claude/skills/impeccable/scripts/*)`.
   Dropped into a plugin's `skills/` dir, that path does not resolve and the tool
   permission does not match — it would break in a way that looks like a plugin bug.
3. **It ships its own installer and hooks** — `npx impeccable install` also writes a
   provider-native hook manifest into `.claude/settings.local.json` that runs its design
   detector on every UI file edit. That is a standing configuration change to the user's
   harness, not a file copy, and it belongs to an explicit user decision — not to
   installing this plugin.

So Impeccable stays an **optional external layer**: if the user has run
`npx impeccable install` themselves, Phase 4 uses it; if not, Phase 4 runs the Emil
motion audit alone and says so in the report. Never block a build on it.

To install it (user's own decision, run from the project root — it will ask about
providers and project-vs-global scope, and install the edit hook):

```bash
npx impeccable install
```

## Refreshing a vendored skill
Re-clone upstream, copy the skill folder over the vendored one, and bump the pinned
commit + date in `VENDOR.md`. Upstream layouts are **not** the `.claude/skills/` shape
often assumed — as of 2026-07-20 both `emilkowalski/skills` and `Leonxlnx/taste-skill`
keep their skills in a top-level `skills/` directory. Check the real layout before copying.
