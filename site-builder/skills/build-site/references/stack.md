# Stack — pinned choices and commands

Dynamic site stack (LPR chose dynamic over static, 2026-07-19).

## Core stack
- **Next.js** (App Router) + **TypeScript** + **Tailwind CSS** — the base.
- **shadcn/ui** — component primitives.
- **Framer Motion** — animation.
- Design intelligence: sibling skills `design-system`, `ui-ux-pro-max`, `ui-styling`.
- Visual generation: `glif` MCP, optional (needs API token).

## Scaffold commands (run in the target parent dir)

```bash
# 1. Next.js app (App Router, TS, Tailwind, src/, import alias)
npx create-next-app@latest <app-name> --ts --tailwind --app --eslint --src-dir --import-alias "@/*" --use-npm

# 2. Animation
cd <app-name>
npm install framer-motion

# 3. shadcn/ui (accept defaults; base color from the Phase-1 design system)
npx shadcn@latest init
```

## Windows / SRVLM01 notes
- Node.js: `C:\Program Files\nodejs` (v24). npm/npx available.
- Glif MCP runs via `npx -y @glifxyz/glif-mcp-server@latest` with `GLIF_API_TOKEN` (plugin `glif_api_token` config). If `npx` stdio flakes on Windows, fall back to marked placeholders — generated visuals are an accelerator, not a hard dependency.
- 21st.dev Magic was dropped 2026-07-20 (LPR decision): its AI generation is paid-plan-only, and the vendored `ui-ux-pro-max` / `ui-styling` skills already cover UI construction. Do not re-add it as a dependency.
- Keep the generated project OFF the `E:\-4-` tooling contour; put site projects in a work/projects location (ask the user; for KEG, inside the KEG working area or a dedicated `sites/` dir they choose).

## Design-token wiring
1. Phase 1 (`design-system` skill) emits primitive→semantic→component tokens.
2. Put them in `tailwind.config.ts` `theme.extend` + `globals.css` CSS variables.
3. shadcn base color + radius should match the chosen palette/scale.

## Deploy (only on explicit user go-ahead)
- Dynamic Next.js → Vercel is the default fit (SSR/ISR). Netlify/Node host also work.
- Deploying externally = separate LPR decision (publishing). Never auto-deploy.

## Verify before "done"
- `npm run build` passes.
- `npm run dev` renders in the preview (drive it — Module_01 п. 6.28).
- Responsive mobile + desktop; light/dark if in scope.
