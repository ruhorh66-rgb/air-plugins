# brief.yaml — the input contract

A `brief.yaml` fully describes one site. Fill it, put content docs next to it, run `/site <folder>`. Fields marked (required) block the build if missing — the skill will STOP-ASK.

```yaml
name: string              # (required) project/site name, e.g. "KEG Partnership"
purpose: string           # (required) what the site is FOR: landing | product showcase | lead-gen | portfolio | ...
audience: string          # (required) who it's for: clients | partners | investors | ...
language: [ru] | [en] | [ru, en]   # (required) content language(s)
pages:                    # (required) list of pages/sections
  - slug: home
    goal: string          # what this page should achieve
    sections: [hero, about, offer, cta, ...]
tone: string              # optional: voice/personality (e.g. "professional, trustworthy")
brand:                    # optional; if absent, the design system is generated from scratch
  logo: path/to/logo.svg  # optional
  colors: [ "#0B3D2E" ]   # optional palette hints (else UI/UX Pro Max picks)
  fonts: [ ]              # optional font hints (else UI/UX Pro Max picks)
  style_hint: string      # optional: e.g. "glassmorphism, gradients" or a named UI/UX Pro Max style
content:                  # where the real copy/content comes from
  docs: [ ./partnership.md, ./offer.md ]   # local files (extract via MarkItDown if DOCX/PDF)
  notes: string           # freeform extra context
constraints:              # optional
  no_publish: true        # if true, never deploy — build local source only
  deploy_target: string   # vercel | netlify | none  (deploy still needs explicit go-ahead)
output_dir: path          # (required) where to scaffold the Next.js project (NOT on E:\-4-)
```

Notes:
- Sensitive documents (contracts, legal) → extract text locally (MarkItDown at `E:\-4-\markitdown`), and decide with the user what is public. Do not put confidential content on a public site.
- If `brand` is empty, Phase 1 generates a fitting design system from the UI/UX Pro Max database and records the choices in `SITE_REPORT.md`.
