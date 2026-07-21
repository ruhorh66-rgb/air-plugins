# site-builder (Claude Code plugin)

Brief-in, website-out. Built for **occasional, zero-recall reuse** — you feed a brief, you get a dynamic website, without remembering the recipe months later. The know-how lives in the bundled skills.

## What's inside
- **skill `build-site`** — the orchestrator recipe (input→output contract, phases). Entry point: `/site <brief-folder>`.
- **vendored design skills** (`ui-ux-pro-max`, `design-system`, `ui-styling`, `design`, `brand`, `slides`, `banner-design`) — pinned copy of the official [UI/UX Pro Max](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill) (MIT). Provides the design database (styles, palettes, font pairings). See `VENDOR.md`.
- **vendored taste/craft skills** — [taste-skill](https://github.com/Leonxlnx/taste-skill), invoked as `design-taste-frontend` (design *direction*, MIT) and [Emil Kowalski's animation skills](https://github.com/emilkowalski/skills) (`emil-design-eng`, `review-animations`, `improve-animations` — motion craft, MIT). Layered on top of the UI/UX Pro Max base: direction at Phase 1, motion at Phase 3, motion audit at Phase 4. See `VENDOR.md`.
- **`impeccable`** — [design quality audit](https://github.com/pbakaus/impeccable) (Apache-2.0). *Not* bundled: it ships its own installer and an edit-time hook. Install it yourself (`npx impeccable install`) and Phase 4 will use it; skip it and the build runs unchanged. See `skills/build-site/references/external-design-skills.md`.
- **`glif` MCP** (`.mcp.json`) — [Glif](https://glif.app), generates site visuals (hero imagery, illustrations, icons, OG images). Needs a token (plugin config `glif_api_token`). Optional — without it the build emits marked placeholders.
- **agent `site-builder`** — runs a build in an isolated context.

## Stack
Dynamic **Next.js (App Router) + TypeScript + Tailwind + shadcn/ui + Framer Motion**. See `skills/build-site/references/stack.md`.

## Use
1. Copy `skills/build-site/assets/brief.template.yaml` → a new folder as `brief.yaml`, fill it, drop content docs beside it.
2. `/site <that-folder>`.
3. Review the built project + `SITE_REPORT.md`. Deploy only when you explicitly decide to.

## Install (local marketplace, same pattern as air-os-governance)
```
claude plugin marketplace add "E:/-7-"
claude plugin install site-builder@air-plugins
```
Already installed? A plugin update only re-copies when the version in `.claude-plugin/plugin.json` changes — bump it, then `claude plugin marketplace update air-plugins && claude plugin update site-builder@air-plugins`. Editing files in `E:\-7-` alone does **not** affect the running plugin; it runs from a cache copy under `~/.claude/plugins/cache/air-plugins/`.
`defaultEnabled: false` — it connects to an external service (glif.app), so it installs disabled; enable it deliberately. Verify with `claude plugin list`.

## Status
v0.2.0 (2026-07-20) — vendored taste + animation layers on top of the v0.1.0 skeleton (2026-07-19). First real run planned: the 031_KEG site. The recipe self-improves — real-build gotchas get appended to `skills/build-site/references/stack.md`.
