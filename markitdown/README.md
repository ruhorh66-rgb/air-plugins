# markitdown (Claude Code plugin)

**Don't burn tokens on a PDF.** Convert on-disk documents — PDF, Word, Excel, PowerPoint, images, HTML, CSV, EPUB, audio, ZIP — to clean Markdown **locally**, then read the Markdown. Cheaper in tokens, keeps tables/headings intact, and never ships a sensitive file to a cloud extractor.

## What's inside
- **skill `markitdown`** — the recipe: when to convert vs. read directly, how to run (CLI or MCP), the OCR path for scanned PDFs, and guardrails. Entry point: `/md <file>`.
- **command `/md`** — one-shot: convert a file and work from the Markdown.

## What's NOT inside (by design)
The MarkItDown tool itself — Microsoft's [`microsoft/markitdown`](https://github.com/microsoft/markitdown) (MIT), a Python package — is **not vendored here**. Per the AIR OS Storage Contour Rule, the runtime (venv/binary, any OCR/Document-Intelligence backend) lives on the private `E:\-4-\markitdown`. This catalog (`E:\-7-`) holds only the shareable *recipe*. The skill points at the local install; it does not reinstall it.

## Prerequisites
- MarkItDown installed on the machine (`pip install markitdown`, or the runtime at `E:\-4-\markitdown`). Check with `markitdown --help`.
- Optional: the `markitdown` / `markitdown-ocr` MCP servers registered in the session (AI_Pipeline `08_MCP_AGENTS`) — the skill uses them when present, else falls back to the CLI.

## Install (local marketplace)
```
claude plugin marketplace add "E:/-7-"
claude plugin install markitdown@air-plugins
```
Or from the remote once pushed:
```
claude plugin marketplace add ruhorh66-rgb/air-plugins
claude plugin install markitdown@air-plugins
```
`defaultEnabled: false` — enable it deliberately after confirming MarkItDown is present on the machine.

## Use
```
/md E:\path\to\report.pdf
```
→ converts locally to `report.pdf.md`, reads that, and proceeds (summarize / extract / feed downstream). For scanned PDFs it falls back to the OCR path and says so.

## Status
v0.1.0 (2026-07-19). Codifies a capability already used across AIR OS (referenced in `site-builder` Phase 0 and Ideas Wiki `IDEA-000002`) into a first-class, installable plugin.
