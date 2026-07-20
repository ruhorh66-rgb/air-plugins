# External design skills (optional taste / craft / audit layers)

The vendored UI/UX Pro Max skills give site-builder a **rules database** (styles,
palettes, font pairings, UX guidelines, a few GSAP presets). They answer "what is
correct." They do not, on their own, carry an opinionated **design direction**, a
deep **animation craft** layer, or a deterministic **quality audit**. Three widely-used
external Claude Code skills fill exactly those gaps. They are **complementary, not
replacements** — keep UI/UX Pro Max as the base and layer these on top.

None of these are vendored into this repo yet (they live under other GitHub owners; this
session could not clone cross-owner repos). This file documents what each adds, where it
plugs into `build-site`, its license, and how to bring it in. `build-site` treats all
three as **optional** — if a skill is not installed, fall back to the vendored path
exactly as it already does for the Magic/Glif MCPs.

## The three skills

| Skill | Repo | License | Layer it adds | Plugs into |
|---|---|---|---|---|
| **Taste Skill** | `Leonxlnx/taste-skill` (tasteskill.dev) | open-source — **confirm LICENSE before vendoring** | "anti-slop" design *direction*: reads the brief, infers a design language, tunes VARIANCE / MOTION / DENSITY dials, ships GSAP code skeletons + a redesign-audit protocol | Phase 1 (design direction) and Phase 3 (build) |
| **Impeccable** | `pbakaus/impeccable` (impeccable.style) | **Apache-2.0** | deterministic design **quality audit + polish**: `/impeccable init\|audit\|polish\|…` (23 commands), a shared `.impeccable/design.json` spec and a Live Mode | Phase 4 (review / polish gate) |
| **Emil animations** | `emilkowalski/skills` | **MIT** | **animation craft**: `emil-design-eng`, `review-animations`, `improve-animations`, `find-animation-opportunities`, `animation-vocabulary`, `apple-design`. Enforces <300ms UI motion, custom easing, "when NOT to animate" | Phase 3 (animation) and Phase 4 (animation audit) |

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

## Bringing them in (run on the machine that owns `E:\-7-`)

Cross-owner repos can't be cloned from inside a scoped Claude Code session, so vendor or
install these locally. Two options per skill:

**A. Runtime-install (fastest, auto-updates):**
```
# Taste Skill and Emil use the skills installer
npx skills@latest add emilkowalski/skills
# Impeccable ships its own installer (detects the .claude harness)
npx impeccable install
```
Runtime-installed skills live in your project or `~/.claude/skills` and are found by
description-match, same as any skill.

**B. Vendor a pinned copy into this plugin (durable, offline — matches VENDOR.md policy):**
1. `git clone` the repo, check out a specific commit.
2. Copy the Claude-Code skill tree (`.claude/skills/<name>/`) into
   `site-builder/skills/<name>/`.
3. Copy the upstream LICENSE into the skill folder.
4. Record source URL, license, version and pinned commit in `site-builder/VENDOR.md`
   (see the "Candidate external design skills" section there).
Only do this for a permissive license you have read: Impeccable (Apache-2.0) and Emil
(MIT) are clear; **Taste Skill's LICENSE must be checked first.**

## How `build-site` uses them (optional-layer contract)
See `../SKILL.md` phases. In short: if the skill is present, invoke it at the phase
noted above; if absent, the existing vendored path runs unchanged. Never block a build on
an external skill, and record in `SITE_REPORT.md` which of these were active.
