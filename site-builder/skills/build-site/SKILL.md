---
name: build-site
description: Build a website end-to-end from a brief — brief-in, site-out. Use when the user wants to create/generate a website, landing page, or marketing site from content + requirements. Scaffolds a dynamic Next.js app, applies a design system via the bundled UI/UX Pro Max skills, generates polished UI with the 21st.dev Magic MCP, adds Framer Motion animation, previews, and hands off the source.
---

# Build Site

Turn a **brief** (what the site is, its content, audience, brand) into a **working dynamic website**. This skill is the orchestrator; the design intelligence lives in the sibling vendored skills (`design-system`, `ui-ux-pro-max`, `ui-styling`, `design`, `brand`) and in the `magic` MCP. You do not need to remember the recipe between projects — it is all here.

## Contract (input → output)

- **Input**: a brief folder containing `brief.yaml` (see `references/brief-schema.md`; template in `assets/brief.template.yaml`) plus any content/source docs (text, images, logos).
- **Output**: a self-contained dynamic **Next.js (App Router) + TypeScript + Tailwind** project in the target dir, buildable and previewable locally, plus a short `SITE_REPORT.md` (what was built, how to run/deploy).

Invocation: `/site <brief-folder>` (see the `site` command) or just describe the site and point at the brief.

## Preconditions (check first, fix if missing)

1. **Node.js** on PATH (`node -v`). On SRVLM01 it is at `C:\Program Files\nodejs` (v24). If missing, stop and tell the user.
2. **Magic MCP** (optional but recommended): needs the `magic_api_key` plugin config (21st.dev). If empty/unset, proceed WITHOUT Magic — build UI from the `ui-ux-pro-max`/`ui-styling` skills + shadcn/ui directly, and say so in the report. Do not block on it.
3. **Design skills present**: the sibling skills `design-system`, `ui-ux-pro-max`, `ui-styling` ship in this plugin — invoke them, do not reinvent their databases.

## Phases

### Phase 0 — Read the brief (STOP-ASK on gaps)
Read `brief.yaml` + content docs. If a required field is missing or ambiguous (purpose, audience, pages, language), ask the user before scaffolding — do not guess. Extract on-disk content locally (e.g. MarkItDown at `E:\-4-\markitdown`) rather than a cloud service, especially for sensitive material.

### Phase 1 — Design system first
Invoke the **`design-system`** skill (and `ui-ux-pro-max` for style/palette/font selection) to produce a tailored design system for this brief: palette, typography pair, spacing/radius scale, component specs → emit as Tailwind theme + CSS variables (`design-tokens`). This is the "designer taste" layer. Pick a concrete style from the UI/UX Pro Max database that fits the brand; record the choice in the report.

### Phase 2 — Scaffold the app
Create a Next.js App-Router + TypeScript + Tailwind project in the target dir (see `references/stack.md` for exact commands and pinned choices). Wire the design tokens from Phase 1 into `tailwind.config` and globals. Add `framer-motion`.

### Phase 3 — Build pages/components
For each page in the brief:
- Generate polished components with the **`magic` MCP** (21st.dev `/ui` — glassmorphism, gradients, motion-ready, shadcn/ui + Tailwind). If Magic is unavailable, hand-build with `ui-styling` + shadcn/ui.
- Fill real content from the brief (never lorem ipsum for final; mark placeholders explicitly if content is missing and ask).
- Apply **Framer Motion** for section transitions/entrance animations (tasteful, not gratuitous).
- Keep it accessible (semantic HTML, contrast per `ui-ux-pro-max` guidelines, keyboard nav).

### Phase 4 — Verify (drive it, don't assume — Module_01 п. 6.28)
`npm run build` must pass; start `npm run dev` and open the preview to confirm it renders (use the browser/preview tools). Fix errors before declaring done. Check responsive (mobile + desktop) and light/dark if in scope.

### Phase 5 — Hand off
Write `SITE_REPORT.md`: what was built (pages, chosen style/palette/fonts, whether Magic was used), how to `npm run dev` / build, and deployment notes. **Publishing/deploying externally is a separate explicit user decision** — do not deploy without it.

## Non-goals / guardrails
- Do not deploy or publish without explicit user go-ahead.
- Do not invent brand assets, legal/marketing copy, or facts — pull from the brief; ask when missing.
- Do not fabricate the design database — use the vendored skills' real data.
- One brief = one site. Re-run per site; this is built for occasional use, not a pipeline.

## Self-improvement
After each real build, if you hit a reusable gotcha or a better step, append it to `references/stack.md` or this file (Core §2.5 self-documentation) so the next project — months later — benefits without you remembering it.

## References
- `references/stack.md` — exact stack, pinned commands, Windows notes.
- `references/brief-schema.md` — the `brief.yaml` contract.
- `references/patterns.md` — **premium motion patterns** (video hero, glass CTA, animated
  headline, particle/aurora backgrounds, tactile micro-interactions, MotionSites.ai +
  video download/remux, "lab" pages for client picks). Read this when a build needs to feel
  impressive/modern, not flat.
- `assets/patterns/` — drop-in React components for the above (HeroVideo, CtaSection,
  LogicChain, Divider, AnimatedHeadline, HeroParticles, CTAButton). Copy into `src/components/`.
- `assets/brief.template.yaml` — copy-and-fill starting point.
