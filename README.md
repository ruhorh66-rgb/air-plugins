# air-plugins — AIR OS plugin/skill catalog (`E:\-7-`)

A Claude Code **plugin marketplace**: shareable plugins and skills, kept deliberately separate from the private runtime, models, and secrets on `E:\-4-`.

## Why a separate catalog (`-7-`)
`E:\-4-` holds the heavy, private runtime — local models (GGUF), HF caches, venvs, binaries (Python/ffmpeg/deno), and `.env` secrets — none of which belongs in git. `E:\-7-` holds only the clean, shareable capability layer (plugin/skill source), so it can be version-controlled and published safely. See the AIR OS Storage Contour Rule.

## Plugins in this catalog
- **site-builder** — brief-in, website-out. Dynamic Next.js scaffolder with a vendored copy of the MIT [UI/UX Pro Max](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill) design skills + shadcn/ui + Framer Motion, with optional [Glif](https://glif.app) MCP for generated visuals. See `site-builder/README.md`.
- **markitdown** — don't burn tokens on a PDF. Convert on-disk documents (PDF, Office, images, …) to clean Markdown **locally** with Microsoft [MarkItDown](https://github.com/microsoft/markitdown) before reading them into the model. Recipe-only (the tool lives on `E:\-4-`, per the Storage Contour Rule). See `markitdown/README.md`.
- **legal-analysis** — verification-first review of legal documents: local text extraction, decomposition into checkable theses, verification of every legal qualification against the primary source, and knowledge cards marked verified or candidate. See `legal-analysis/README.md`.

## Standalone utilities (not Claude Code plugins)

These are plain scripts, not plugins — they are **not** listed in `marketplace.json` and
are not installed via `claude plugin install`. They live here because the code is
shareable even though what it processes is not.

- **llm-queue** — a job queue for a local `llama-server`: priorities, restart survival,
  retry, and visibility on top of an existing slot semaphore. Code here, state (queue db,
  results, `llm_client.py`) stays in the private runtime via `LLM_QUEUE_DATA_DIR`. See
  `llm-queue/README.md`.

## Install (this machine, from the local catalog)
```
claude plugin marketplace add "E:/-7-"
claude plugin install site-builder@air-plugins
```
Or from the remote once pushed:
```
claude plugin marketplace add ruhorh66-rgb/air-plugins
claude plugin install site-builder@air-plugins
```

## Rules
- Only shareable plugin/skill source here — **no secrets, no models, no venvs** (`.gitignore` enforces).
- Vendored third-party material is pinned with its LICENSE + provenance (see each plugin's `VENDOR.md`), taken from official upstream repos.
