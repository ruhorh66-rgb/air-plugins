# Vendored dependencies

## UI/UX Pro Max design skills

The `skills/` folders `ui-ux-pro-max`, `design-system`, `ui-styling`, `design`, `brand`, `slides`, `banner-design` are a **pinned copy** of the official upstream skill:

- Source: https://github.com/nextlevelbuilder/ui-ux-pro-max-skill
- License: **MIT** (Copyright (c) 2024 Next Level Builder) — retained at `skills/UI-UX-PRO-MAX-LICENSE.txt`.
- Vendored version: **2.11.0**
- Vendored commit: `f8ac5e1266dba8354ea96e19994d9f4345e7ec31`
- Vendored on: 2026-07-19, from the repo's `.claude/skills/` tree.

Why vendored (not runtime-installed): durability. This capability is used occasionally; a pinned copy guarantees it works months later without network access or an installer step ([[e4-local-tooling-durable-layout]] durable principle). To update, re-clone upstream and refresh these folders, bumping the commit/version above.

## Taste Skill (design direction)

The `skills/design-taste-frontend` folder is a **pinned copy** of the upstream skill:

- Source: https://github.com/Leonxlnx/taste-skill
- License: **MIT** (Copyright (c) 2026 Leonxlnx) — retained at `skills/TASTE-SKILL-LICENSE.txt`.
- Vendored commit: `98565e65bc3274ddf6eb0838734341714057178b`
- Vendored on: 2026-07-20, from the repo's top-level `skills/taste-skill/` tree.

Layout note: upstream keeps its skills in a top-level `skills/` directory, **not** `.claude/skills/`. The repo ships 14 skill folders (`brutalist-skill`, `minimalist-skill`, `redesign-skill`, …); only `taste-skill` is vendored here — it is the direction-setter `build-site` Phase 1 uses. The others are style-specific variants we do not want fighting the UI/UX Pro Max style pick.

Folder renamed on vendoring: upstream ships it as `skills/taste-skill/` but its frontmatter declares `name: design-taste-frontend`. Every other skill in this plugin has folder == `name`, so the folder was renamed to match and the file contents left byte-identical to upstream. Invoke it as **`design-taste-frontend`**.

## Emil Kowalski animation skills (motion craft)

The `skills/` folders `emil-design-eng`, `review-animations`, `improve-animations` are a **pinned copy** of the upstream skills:

- Source: https://github.com/emilkowalski/skills
- License: **MIT** (Copyright (c) 2026 Emil Kowalski) — retained at `skills/EMIL-SKILLS-LICENSE.txt`.
- Vendored commit: `6bf24434f7730ad169077756cf9c7cd7bd675fc6`
- Vendored on: 2026-07-20, from the repo's top-level `skills/` tree.

Upstream also ships `animation-vocabulary`, `apple-design`, `find-animation-opportunities` — not vendored, since `build-site` only needs the build-time guide (`emil-design-eng`) and the two audit skills. Add them the same way if a build ever needs them.

## Impeccable (design audit) — deliberately NOT vendored

External, optional. It ships its own installer (`npx impeccable install`), hard-codes `.claude/skills/impeccable/` paths in its `SKILL.md` and `allowed-tools`, weighs 2.5 MB, and installs an edit-time hook into `.claude/settings.local.json`. Vendoring it into this plugin would break its own setup step and silently change the user's harness. Reasoning in full: `skills/build-site/references/external-design-skills.md`. License confirmed **Apache-2.0** by reading upstream `LICENSE` on 2026-07-20 (commit `4d849eb75f216109ea7053ed21530a11fafcc786`) — safe to vendor if that decision is ever revisited.

## Glif MCP

Declared in `.mcp.json`, run via `npx -y @glifxyz/glif-mcp-server@latest` (official). Not vendored — it's a hosted service needing an API token (`glif_api_token`). Config taken from the official repo (https://github.com/glifxyz/glif-mcp-server), verified 2026-07-19.

## Removed: Magic MCP (21st.dev)

Declared here until 2026-07-20, then dropped by LPR decision: 21st.dev gates AI generation behind paid plans (search is free, installs are capped at 2/day), and the vendored UI/UX Pro Max skills already cover UI construction — a paid second path to the same result. Recorded so the removal reads as a decision, not an omission.
