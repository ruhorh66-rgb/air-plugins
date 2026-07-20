# Idea Card template

Copy, fill, and save as `01_CARDS/IDEA-NNNNNN_slug.md` in the `002_Ideas_Wiki` vault.
`NNNNNN` = the next sequential number (read `01_CARDS/` first).

```markdown
---
card_id: IDEA-NNNNNN
card_type: idea
source_type: video
source_title: ""
source_author: ""
source_url: ""
captured_at: YYYY-MM-DD
relevant_projects: []
tags: []
---

# Короткое название идеи

## Что это

Краткое описание идеи/фрейма без домыслов сверх того, что реально сказано в источнике.

## Ключевые тезисы

-
-

## Применимость к нашим проектам

- **<Project>** — почему релевантно (конкретно, не общими словами).

## Пробелы / чего у нас нет

- (если применимо)

## Ссылки

- Источник: <url>
```

For a source with several independent ideas, cut one atomic card per idea and add an
overview card with `card_type: idea_overview` that lists them under a `Дочерние карточки`
section, plus a `Смежные карточки` list linking related existing cards.
