# idea_card_gate

The gate an Idea Card must pass before it is accepted (else `status: needs_review`).

```yaml
gate_id: idea_card_gate
required:
  - card_id_exists                 # sequential IDEA-NNNNNN, from max in 01_CARDS/ + 1
  - source_url_or_local_path_exists
  - relevant_projects_non_empty
  - "применимость к нашим проектам" section present and non-generic
else_status: needs_review
```

## Why the relevance gate is strict

An idea without an explanation of *why it is ours* does not earn a place in this base. The
vault is planning fuel, not a clipping archive — the whole value is the link back to real
projects. A card whose applicability section is a generic sentence ("useful for AI work")
is exactly the failure this gate blocks.

## Relevance keys (`relevant_projects`)

Keys come from `AIR_VAULT_INDEX.md` § Vault Map. Known keys seen in use include:
`AI_Pipeline`, `GGG_Resource`, `Bitrix_Control_Platform`, `Labs`, `CKBA`, `GPN_Orenburg`,
`RSU`, `MRK`, `HR`, `KEG`, `Litvinov`, `Pokrovsky`, `AIR_OS`.

If unsure of the exact spelling of a key:
1. still put your best value in `relevant_projects`, and
2. in the body, write the full project name + its `PROJ-...` id (from
   `000_Registry_Wiki/00_PROJECTS`) so a human can resolve it regardless.

Never leave `relevant_projects` empty to "fix later" — that is exactly what fails the gate.

## Non-generic check — what passes

- PASS: "**RSU** — ставка ЦБ нужна для расчёта процентов по ст.395 ГК на долг 22M".
- FAIL: "**RSU** — может пригодиться".

Each bullet names a concrete mechanism, artifact, stage or number tying the idea to that
project. If you cannot write that, the idea may simply not be relevant to that project —
drop the project rather than pad the list.

## Поле `gtd` (GTD-статус, не часть гейта)

Каждая idea-карточка несёт `gtd: someday | next | promoted:TASK-OBS-XXXX` (по умолчанию
`someday`). Это GTD-метка (002_Ideas = Someday/Maybe); гейт её не требует, но скил проставляет
`someday` по умолчанию. При промоушене идеи в работу — завести задачу в 010 (TASK-OBS через
Master Task Template) и поставить `gtd: promoted:TASK-OBS-XXXX`. Стандарт: 010 `STD-010-011`.
