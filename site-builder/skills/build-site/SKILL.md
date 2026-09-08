---
name: build-site
description: Build a website end-to-end from a brief — brief-in, site-out. Use when the user wants to create/generate a website, landing page, or marketing site from content + requirements. Scaffolds a dynamic Next.js app, applies a design system via the bundled UI/UX Pro Max skills, builds UI with shadcn/ui + Tailwind, generates matching visuals with the Glif MCP, adds Framer Motion animation, previews, and hands off the source.
---

# Build Site

Turn a **brief** (what the site is, its content, audience, brand) into a **working dynamic website**. This skill is the orchestrator; the design intelligence lives in the sibling vendored skills (`design-system`, `ui-ux-pro-max`, `ui-styling`, `design`, `brand`). You do not need to remember the recipe between projects — it is all here.

## Contract (input → output)

- **Input**: a brief folder containing `brief.yaml` (see `references/brief-schema.md`; template in `assets/brief.template.yaml`) plus any content/source docs (text, images, logos).
- **Output**: a self-contained dynamic **Next.js (App Router) + TypeScript + Tailwind** project in the target dir, buildable and previewable locally, plus a short `SITE_REPORT.md` (what was built, how to run/deploy).

Invocation: `/site <brief-folder>` (see the `site` command) or just describe the site and point at the brief.

## Preconditions (check first, fix if missing)

1. **Node.js** on PATH (`node -v`). On SRVLM01 it is at `C:\Program Files\nodejs` (v24). If missing, stop and tell the user.
2. **Glif MCP** (optional): needs the `glif_api_token` plugin config (glif.app). Covers generated visuals — hero imagery, illustrations, icons, OG images. If empty/unset, proceed WITHOUT Glif: use explicit, clearly-marked placeholders and list them in the report so the user can supply real assets. Do not block on it, and never silently ship a placeholder as final.
3. **Design skills present**: the sibling skills `design-system`, `ui-ux-pro-max`, `ui-styling` ship in this plugin — invoke them, do not reinvent their databases.
4. **Taste/craft layers present**: `design-taste-frontend` (design direction) and Emil Kowalski's `emil-design-eng` (build-time motion guide) + `improve-animations` (motion audit) are **vendored in this plugin** — treat them as available, same as the skills above. `review-animations` is vendored too but is **user-invocable only** (`disable-model-invocation: true` upstream) — you cannot call it; suggest it, do not schedule it. `impeccable` (quality audit/polish) is **not** vendored: it ships its own installer and hooks, so it is genuinely optional — use it at Phase 4 only if the user has installed it, and never block on it. See `references/external-design-skills.md`.

## Phases

### Phase 0 — Read the brief (STOP-ASK on gaps)
Read `brief.yaml` + content docs. If a required field is missing or ambiguous (purpose, audience, pages, language), ask the user before scaffolding — do not guess. Extract on-disk content locally (e.g. MarkItDown at `E:\-4-\markitdown`) rather than a cloud service, especially for sensitive material.

### Phase 1 — Design system first
Invoke the **`design-system`** skill (and `ui-ux-pro-max` for style/palette/font selection) to produce a tailored design system for this brief: palette, typography pair, spacing/radius scale, component specs → emit as Tailwind theme + CSS variables (`design-tokens`). This is the "designer taste" layer. Pick a concrete style from the UI/UX Pro Max database that fits the brand; record the choice in the report.

**Design direction (`design-taste-frontend`).** Run it *before* freezing the tokens: it reads the brief, infers a design language and tunes the VARIANCE / MOTION / DENSITY dials. Let it choose the direction; keep `ui-ux-pro-max` as the accessibility/rules check on top. Pick **one** direction-setter (`design-taste-frontend` **or** the UI/UX Pro Max style pick) — do not impose two design languages at once. Record the chosen dials in the report.

### Phase 2 — Scaffold the app
Create a Next.js App-Router + TypeScript + Tailwind project in the target dir (see `references/stack.md` for exact commands and pinned choices). Wire the design tokens from Phase 1 into `tailwind.config` and globals. Add `framer-motion`.

### Phase 3 — Build pages/components
For each page in the brief:
- Build components with **`ui-styling` + shadcn/ui + Tailwind**, following the Phase 1 design system. Reach for `assets/patterns/` and `references/patterns.md` when a section needs to feel premium rather than flat.
- Fill real content from the brief (never lorem ipsum for final; mark placeholders explicitly if content is missing and ask).
- Apply **Framer Motion** for section transitions/entrance animations (tasteful, not gratuitous).
- **Animation craft (`emil-design-eng`).** Use it to guide the motion: UI animations under ~300ms, custom easing (not CSS defaults), and its "when NOT to animate" rule. This owns the motion axis — it is the single source of animation taste over Framer Motion.
- Keep it accessible (semantic HTML, contrast per `ui-ux-pro-max` guidelines, keyboard nav).

### Phase 3b — Visual assets (Glif)
Components from Phase 3 are structure; this step fills the imagery they frame. For each visual the brief calls for (hero, section illustrations, icons, OG/social preview):
- Find a fitting workflow with `search_workflows` / `list_featured_workflows`, then `run_workflow`. Prefer one workflow family across the whole site so the visuals read as one set, not a stock-image grab bag.
- Feed the Phase 1 design system into the prompt (palette, mood, style name) — the generated art must sit inside the same system as the type and color, not beside it.
- Save into the project (e.g. `public/images/`) and reference locally. Do not hotlink generated URLs — they expire.
- Record in the report which images are generated vs. supplied by the user. Generated art is a **draft asset**: brand-critical marks (logo, legal/marketing imagery) are never generated — ask for the real thing.

If the Glif token is unset, skip generation and emit marked placeholders instead (see Precondition 2).

### Phase 4 — Verify (measure it, don't claim it)
`npm run build` must pass; start `npm run dev` and open the preview to confirm it renders. Fix errors before declaring done.

**Then run the gates — they are a script, not a judgement:**

```
node scripts/gates.mjs --url http://localhost:3000 --out ./
```

It writes `GATES.json` (machine) and `GATES.md` (human): accessibility (axe, pa11y), Core Web Vitals (Lighthouse/Unlighthouse, mobile profile), broken links (lychee), schema.org incl. JobPosting (sdtt), sitemap (xmllint), and keyboard focus visibility (Playwright, `scripts/keyboard-focus.mjs`).

Read the three rules the script enforces, because they decide what you may report:
- a missing tool is `not_run` with a reason — never a silent skip;
- a tool that ran but produced unparseable output is `fail`, never `pass` — unreadable is not success;
- exit code 1 if anything failed. A `not_run`-only run exits 0 but carries a warning banner: it is not a clean pass.

**Never report a gate as passed from your own reading of the page.** The numbers come from `GATES.json` or they do not exist — a model's impression of contrast, performance or link health is not evidence.

**Design + motion audit (checkers, not generators).** After the build renders:
- **`improve-animations`** (vendored, always run) — invoke it on the built project. It is **read-only by design**: it emits a prioritized `AUDIT.md` plus self-contained implementation plans, and applies nothing. **You** then apply the plans it produced — do not report the audit as if it were the fix.
- **`impeccable`** (optional, only if the user installed it) — `/impeccable audit` then `/impeccable polish` for a deterministic design-quality pass. Use its audit/polish only; do NOT let it re-impose a second design language over Phase 1's direction.
- **`review-animations`** — do **not** try to invoke this one: upstream ships it with `disable-model-invocation: true`, so only the user can run it, as `/review-animations`. It reviews a diff against Emil's `STANDARDS.md`. Offer it as a manual gate before hand-off; never block on it.
Record which audits ran, which plans you applied, and their headline findings in the report.

### Phase 5 — Hand off
Write `SITE_REPORT.md`: what was built (pages, chosen style/palette/fonts), which images are Glif-generated vs. user-supplied vs. still placeholders, how to `npm run dev` / build, and deployment notes. **Publishing/deploying externally is a separate explicit user decision** — do not deploy without it.

## Delegation and economy

Three roles live in `agents/`, each with its model fixed under the task, not under habit:

| Role | Model | Why |
|---|---|---|
| `site-page-builder` | haiku | one page from ready tokens and ready content; volume, not judgement |
| `site-designer` | sonnet | design direction, palette, typography — judgement |
| `site-reviewer` | opus | independent verdict before hand-off; reads `GATES.json`, fixes nothing |

Two economy rules that matter more than the model choice:
- **the cheapest check is a script, not a model.** Facts come from `gates.mjs`; a subagent is for judgement. Delegating a measurement is how you get a self-report instead of a number;
- **a subagent keeps its own prompt cache and it expires in five minutes by default.** When calling the same role repeatedly — page after page — set `cacheTtl: 1h` in its definition, or every call warms from cold.

There is no programmatic API for the cost of one subagent call; that is documented as absent. Read `/usage` and append a row to `docs/ECONOMY_LOG.md` after each run.

## Non-goals / guardrails
- Do not deploy or publish without explicit user go-ahead.
- Do not invent brand assets, legal/marketing copy, or facts — pull from the brief; ask when missing.
- Do not fabricate the design database — use the vendored skills' real data.
- One brief = one site. Re-run per site; this is built for occasional use, not a pipeline.

## Self-improvement
After each real build, if you hit a reusable gotcha or a better step, append it to `references/stack.md` or this file (Core §2.5 self-documentation) so the next project — months later — benefits without you remembering it.

## References
- `references/external-design-skills.md` — the **taste/craft/audit layers** (`design-taste-frontend`, Emil animations, `impeccable`): what each adds, which phase it plugs into, licenses, and which are vendored vs. external.
- `references/stack.md` — exact stack, pinned commands, Windows notes.
- `references/brief-schema.md` — the `brief.yaml` contract.
- `references/patterns.md` — **premium motion patterns** (video hero, glass CTA, animated
  headline, particle/aurora backgrounds, tactile micro-interactions, MotionSites.ai +
  video download/remux, "lab" pages for client picks). Read this when a build needs to feel
  impressive/modern, not flat.
- `assets/patterns/` — drop-in React components for the above (HeroVideo, CtaSection,
  LogicChain, Divider, AnimatedHeadline, HeroParticles, CTAButton). Copy into `src/components/`.
- `assets/brief.template.yaml` — copy-and-fill starting point.

## Цикл

```text
цель   — вёрстка использует токены дизайн-системы, а не произвольные значения
гейт   — python skills\design-system\scripts\html-token-validator.py <файл.html>
предел — 5 кругов
показ  — один итог: что не проходило, что исправлено, чем подтверждено
```

Канонический текст цикла — скилл `verification-loop` (плагин `air-loop`, `E:\-7-\air-loop`). Раннер, считающий круги: `E:\-7-\air-loop\scripts\loop_run.py`.
