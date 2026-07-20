# Extraction

Text comes out **locally**. The document under review is not uploaded to a cloud
converter — legal material is frequently privileged, and conversion is the one step
that would leak the whole file.

Web access during a review is used only to read published legal sources. That is a
different direction of travel: public text comes in, the client's document never goes out.

## By format

| Input | Method | Watch for |
|---|---|---|
| `.docx`, `.pptx`, `.xlsx` | Local markitdown-class converter | Slide decks lose layout meaning — see below |
| `.pdf` with text layer | Same | Multi-column layouts interleave; check ordering |
| `.pdf` scanned, images | Local OCR fallback (rus+eng where relevant) | Verify the yield before trusting it |
| `.eml` / message bodies | Read body and attachment list separately | The body is often empty and the substance is entirely in the attachment |

## Verify the yield

Record the extraction method and character count. Then sanity-check:

- A 13-slide deck yielding ~200 characters means extraction failed, not that the deck is
  empty. Re-run with OCR before concluding anything.
- A scan yielding fluent text with no artifacts may mean the OCR silently fell back to
  a text layer that belongs to a different page.
- Compare the character count against the visible size of the document.

Never review an empty extraction and report it as a thin document.

## Encoding

Console pipelines mangle Cyrillic under legacy code pages. Write extracted text to a
UTF-8 file and read the file, rather than piping converter output through a terminal
that may re-encode it. A `UnicodeEncodeError` at print time usually means extraction
already succeeded — do not re-run the conversion, fix the output path.

## Decks specifically

Presentation format destroys reasoning structure by design: a slide compresses an
argument to its conclusion. When reviewing a deck, expect:

- claims without their supporting chain — the chain is not necessarily absent from the
  author's thinking, only from the artifact;
- status labels ("proved", "not proved") stripped of the qualifications that belonged
  with them;
- code references and shorthand with no legend on the slide.

Do not treat an unsupported deck claim as an author error automatically. Ask for the
text version. Where the format itself is the problem, say so — a deck is the wrong
container for work-product that will be reasoned over.

## Originals

Hash the original (SHA-256) and record path, size and hash on the source card.

Before writing a binary anywhere version-controlled, confirm the target path is covered
by ignore rules and check the result — do not assume. Raw originals of privileged
material do not belong in a repository with a remote.
