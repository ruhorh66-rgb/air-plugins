---
description: Convert an on-disk document (PDF/Office/image/…) to Markdown locally with MarkItDown, then read the Markdown instead of the raw file — to save tokens.
argument-hint: "<file-path> [output.md]"
---

Convert this file to Markdown locally, then work from the Markdown: **$ARGUMENTS**

Use the `markitdown` skill. Steps:
1. If `$ARGUMENTS` is empty, ask for the file path.
2. If the target is already plain text/Markdown/code, say so and just read it — skip conversion.
3. Otherwise convert **locally** with MarkItDown (CLI `markitdown "<file>" -o "<file>.md"`, or the `markitdown` MCP tool if registered). Do not upload the raw file to a cloud extractor.
4. Read the produced `.md` (not the original binary) and proceed with whatever the user asked (summarize / extract / feed downstream).
5. If the output is empty or garbled, the document is probably scanned — rerun via the OCR path (`markitdown-ocr`). Report whether OCR was used.
