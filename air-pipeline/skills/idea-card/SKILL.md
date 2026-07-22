---
name: idea-card
description: Create an atomic, cross-project Idea Card in the 002_Ideas_Wiki vault from an external input (video, article, post) that could apply to several AIR OS projects. Enforces the mandatory relevance gate — every card must name which projects it is relevant to and why, in a non-generic applicability section. Use when an external source is analyzed substantively (not merely watched/read) and is not tied to a single project. Do NOT use for a source that belongs to one project (that is a Source Card inside that project's vault, with chain-of-custody) or for a file the user only wants read/summarized.
---

# Idea Card

Turn an external input that was **actually analyzed** (a video, article, post, repo) into
one or more **atomic Idea Cards** in the cross-project `002_Ideas_Wiki` vault. An Idea Card
is not an archive of a source — it is a piece of **planning raw material**: an idea or
framework worth keeping in reach when planning work across projects, with an explicit
statement of *which projects it helps and why*.

## Idea Card vs Source Card — pick the right one first

- **Source Card** lives **inside** a project vault, is tied to one `project_id`, and carries
  chain-of-custody (`source_sha256`, verbatim quotes). It is evidence about one case. If the
  material belongs to one project, this is NOT the skill — use the project's Source Card
  standard (or the `legal-analysis` plugin for legal work-product).
- **Idea Card** lives here, in the separate cross-project vault, and **must** state which
  projects the idea is relevant to and why. It is not proof — it is planning fuel.

If in doubt, ask which one the user wants before writing.

## Where cards go

- Vault: `002_Ideas_Wiki`. Resolve its location from the `ideas_vault_path` config; if unset,
  ask (or, in a Claude Code session that already has the repo in scope, target it by its git
  remote on the designated working branch — do not push to the default branch).
- Folder: `01_CARDS/`. File name: `IDEA-NNNNNN_slug.md`.
- **Numbering is sequential across the whole vault.** List `01_CARDS/` and take `max(NNNNNN)+1`.
  Never reuse or guess a number — read the directory first.

## Required frontmatter

```yaml
---
card_id: IDEA-NNNNNN
card_type: idea            # or idea_overview for a multi-card source (see below)
source_type: video / article / post / other
source_title: "..."
source_author: "..."
source_url: "..."
captured_at: YYYY-MM-DD
relevant_projects: [Vault_Map_key, ...]   # MANDATORY, non-empty — see references/gate.md
tags: []
gtd: someday   # someday | next | promoted:TASK-OBS-XXXX  (GTD; default someday — see 010 STD-010-011)
---
```

`relevant_projects` uses keys from `AIR_VAULT_INDEX.md` § Vault Map (e.g. `AI_Pipeline`,
`GGG_Resource`, `Bitrix_Control_Platform`, `Labs`, `CKBA`, `HR`, `RSU`, …). If you are not
certain of a key's exact spelling, still fill it with your best value **and** spell the full
project name + `PROJ-...` id in the body so the reference is unambiguous — cross-vault links
here are plain text, never wikilinks (Obsidian does not resolve `[[...]]` across independent
vaults without a plugin we don't have).

## Required body sections

```markdown
# Short idea title

## Что это
Brief description of the idea/framework — no claims beyond what the source actually says.

## Ключевые тезисы
- …

## Применимость к нашим проектам
- **<Project>** — why it is relevant (concrete, not general words).

## Пробелы / чего у нас нет
- (recommended — honestly note if the idea is NOT covered by our stack instead of forcing a link)

## Ссылки
- Источник: <url>
```

`Применимость к нашим проектам` is mandatory and must be **specific** — it duplicates
`relevant_projects` in human-readable form *with a reason per project*. A generic sentence
fails the gate.

## The gate (do not skip)

Before writing, confirm the card passes `idea_card_gate` (full text in `references/gate.md`):

1. `card_id` exists and is the next sequential number;
2. `source_url` **or** a local path exists;
3. `relevant_projects` is non-empty;
4. the "Применимость к нашим проектам" section is present and **non-generic**.

If any check fails, the card is `needs_review`, not accepted.

## Atomicity — cut more cards, not one long one

- One card = one idea you could cite on its own.
- If a source carries several independent ideas/frameworks, cut **separate atomic cards**,
  not one long summary.
- A multi-idea source also gets an **overview card** (`card_type: idea_overview`) that lists
  its child cards. Link children and related cards by text in a `Ссылки` / `Смежные карточки`
  section. An island in the graph is an unfinished job — connect each new card to related
  existing ones.

## Recipe

1. Confirm this is cross-project material (else → Source Card). Confirm the source was
   analyzed by substance, not just opened.
2. Resolve the vault (`ideas_vault_path` or ask / in-session repo). Read `01_CARDS/` to get
   the next `IDEA-NNNNNN`.
3. Draft frontmatter + body. Fill `relevant_projects` and the applicability section with a
   concrete reason per project — check the project registry (`000_Registry_Wiki/00_PROJECTS`)
   if you are unsure what a project is, rather than inventing relevance.
4. Run the gate. If it fails, fix or mark `needs_review`.
5. If the source has several ideas, split into atomic cards + an `idea_overview`.
6. Write to `01_CARDS/` on the designated working branch. Do not invent facts about the
   source; if you did not verify something, say so in the body.

## References
- `references/gate.md` — the full gate and the relevance-key guidance.
- `references/template.md` — the copy-and-fill card template.
