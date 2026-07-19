# Vendored dependencies

## UI/UX Pro Max design skills

The `skills/` folders `ui-ux-pro-max`, `design-system`, `ui-styling`, `design`, `brand`, `slides`, `banner-design` are a **pinned copy** of the official upstream skill:

- Source: https://github.com/nextlevelbuilder/ui-ux-pro-max-skill
- License: **MIT** (Copyright (c) 2024 Next Level Builder) — retained at `skills/UI-UX-PRO-MAX-LICENSE.txt`.
- Vendored version: **2.11.0**
- Vendored commit: `f8ac5e1266dba8354ea96e19994d9f4345e7ec31`
- Vendored on: 2026-07-19, from the repo's `.claude/skills/` tree.

Why vendored (not runtime-installed): durability. This capability is used occasionally; a pinned copy guarantees it works months later without network access or an installer step ([[e4-local-tooling-durable-layout]] durable principle). To update, re-clone upstream and refresh these folders, bumping the commit/version above.

## Magic MCP (21st.dev)

Declared in `.mcp.json`, run via `npx -y @21st-dev/magic@latest` (official). Not vendored — it's a hosted service needing an API key (`magic_api_key`). Config taken from official 21st.dev docs (https://21st.dev/magic), verified 2026-07-19.
