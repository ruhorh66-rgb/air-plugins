# air-pipeline (Claude Code plugin)

AIR OS knowledge-pipeline skills — the repeatable procedures that turn inputs into
gated, registered wiki cards. Built for the same "zero-recall reuse" as the rest of this
catalog: the standard lives in the skill, not in your head.

## What's inside (v0.1.0)

- **skill `idea-card`** — create atomic, cross-project **Idea Cards** in `002_Ideas_Wiki`
  from external inputs (video/article/post), enforcing the relevance gate
  (`relevant_projects` non-empty + a non-generic "Применимость к нашим проектам" section).
  This packages the procedure used by hand to build IDEA-000001…000023.

## Deliberately not shipped yet

The other pipeline steps are **roadmapped, not stubbed** — see `ROADMAP.md`. They encode
procedures that live on the private runtime (`vault_init.ps1`, the graphify config, the
Source Card standard, the Vault Map). Writing them from memory would invent a procedure —
exactly the failure mode (`LAB-CTR-0009`) this system is built to prevent. Each is listed
with the one source document it needs before it can be authored faithfully.

## What is NOT here (already covered elsewhere)

- **Source Cards / verification-first review of legal work-product** → the `legal-analysis`
  plugin already does this (local extraction, thesis decomposition, per-qualification
  verification, verified/candidate cards, learning store). `air-pipeline` does not duplicate it.
- **Local document → Markdown intake** → the `markitdown` plugin.

## Config

`ideas_vault_path` (optional) — absolute path to the local `002_Ideas_Wiki` vault, kept
outside this public repo. If unset, `idea-card` asks where to write, or targets the vault by
its git remote when run in a session that has it in scope (on the designated working branch,
never the default branch).

## Status

v0.1.0 (2026-07-20). First skill: `idea-card`. `defaultEnabled: false`.
