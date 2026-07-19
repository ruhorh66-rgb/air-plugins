---
description: Build a website from a brief folder (brief.yaml + content) using the build-site skill.
argument-hint: "<brief-folder>"
---

Build a website from the brief at: **$ARGUMENTS**

Use the `build-site` skill. Steps:
1. Read `$ARGUMENTS/brief.yaml` and the content docs it references. If `$ARGUMENTS` is empty or has no `brief.yaml`, point the user to `skills/build-site/assets/brief.template.yaml` and ask them to fill one in.
2. Follow the `build-site` skill phases 0–5 (design system → scaffold Next.js → build pages with Magic MCP + UI/UX Pro Max + Framer Motion → verify build/preview → hand off `SITE_REPORT.md`).
3. STOP-ASK on any missing required brief field. Never deploy/publish without explicit user go-ahead.
