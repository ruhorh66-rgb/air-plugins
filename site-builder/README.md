# site-builder (Claude Code plugin)

Brief-in, website-out. Built for **occasional, zero-recall reuse** — you feed a brief, you get a dynamic website, without remembering the recipe months later. The know-how lives in the bundled skills.

## What's inside
- **skill `build-site`** — the orchestrator recipe (input→output contract, phases). Entry point: `/site <brief-folder>`.
- **vendored design skills** (`ui-ux-pro-max`, `design-system`, `ui-styling`, `design`, `brand`, `slides`, `banner-design`) — pinned copy of the official [UI/UX Pro Max](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill) (MIT). Provides the design database (styles, palettes, font pairings). See `VENDOR.md`.
- **`magic` MCP** (`.mcp.json`) — [21st.dev Magic](https://21st.dev/magic), generates polished shadcn/Tailwind UI. Needs an API key (plugin config `magic_api_key`, from https://21st.dev/magic/console). Optional — without it the design skills + shadcn still build the site.
- **agent `site-builder`** — runs a build in an isolated context.

## Stack
Dynamic **Next.js (App Router) + TypeScript + Tailwind + shadcn/ui + Framer Motion**. See `skills/build-site/references/stack.md`.

## Use
1. Copy `skills/build-site/assets/brief.template.yaml` → a new folder as `brief.yaml`, fill it, drop content docs beside it.
2. `/site <that-folder>`.
3. Review the built project + `SITE_REPORT.md`. Deploy only when you explicitly decide to.

## Install (local marketplace, same pattern as air-os-governance)
```
claude plugin marketplace add "E:/-4-/site-builder"
claude plugin install site-builder@site-builder-local
```
`defaultEnabled: false` — it connects to an external service (21st.dev), so it installs disabled; enable it deliberately. Verify with `claude plugin list`.

## Status
v0.1.0 skeleton (2026-07-19). First real run planned: the 031_KEG site. The recipe self-improves — real-build gotchas get appended to `skills/build-site/references/stack.md`.
