# air-pipeline roadmap

Skills identified as genuinely missing from the AIR OS toolset (i.e. NOT already covered by
`legal-analysis` or `markitdown`). Each is listed with the **one source it needs** before it
can be authored faithfully. They are not shipped as stubs on purpose: encoding a procedure
from memory would fabricate it, which is the exact failure this system guards against.

## Tier 0/1 — core pipeline & governance

| Skill | What it would do | Needs (source of truth) |
|---|---|---|
| `vault-init` | Initialize a new project vault from the template + register it in `000_Registry_Wiki` (index row + junction) | `PROJECT_VAULT_TEMPLATE`, `PROJECT_SETUP_CHECKLIST.md`, and the real `vault_init.ps1` |
| `raw-intake` | L1 registration: File Registry + `manifest` + `run_log` + local normalization (via markitdown) | the L1 layout spec (`04_SOURCE_CARDS` / File Registry format) + a run_log example |
| `graphify` | Run the graphify pass, report nodes/edges delta, update `last_graphify_commit` in the registry entry | the `.graphify` config + how graphify is invoked |
| `cross-link` | Register a cross-vault relationship as plain text (not wikilinks) + update the MOC/registry entry | the registry MOC/junction convention (partly in `000_Registry_Wiki`) |
| `session-handoff` | Emit a `SESSION_HANDOFF` / `MIGRATION_REPORT` at end of session | an accepted handoff/migration-report example (e.g. `SESSION_HANDOFF_2026-07-20.md`) |

## Tier 2 — domain (legal / HR), complements legal-analysis, does not duplicate it

| Skill | What it would do | Needs |
|---|---|---|
| `ru-case-data` | Pull RU legal/business data (ЕГРЮЛ / kad.arbitr / ФССП / ЕФРСБ / ЦБ rate) with a ПДн mode | the source list + access decision (see 002_Ideas_Wiki IDEA-000016) |
| `legal-deadline` | Compute procedural/contract deadlines on the RU production calendar (isdayoff) | confirmation of the calendar source + a worked example |
| `legal-interest` | Compute interest/penalty under ст.395 ГК from the ЦБ key-rate history | the calculation convention used in RSU/GPN cards |
| `hr-screening` | Candidate screening per HR-5.1 with ПДн compliance | `030_HR_Living_Document.md` §2 (targets) + the screening rubric |

## Note on Source Cards

A generic `source-card` skill is intentionally NOT on this list: for legal work-product the
`legal-analysis` plugin already produces verified/candidate cards with chain-of-custody. If a
non-legal Source Card standard is needed, it would be a separate skill keyed to
`SOURCE_CARD_STANDARD_v0.1` — provide that document first.
