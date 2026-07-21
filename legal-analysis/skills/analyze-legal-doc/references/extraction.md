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

## Scans: the OCR branch

A scanned original has no text layer, so the converter must fall back to OCR — locally,
never to a cloud OCR service. The chain is rasterise → OCR per page → concatenate:

```text
скан (PDF без текстового слоя)
  → pdftoppm -png -r 300            (растеризация постранично)
  → tesseract -l rus+eng            (распознавание)
  → companion .txt (UTF-8) + SHA-256 оригинала
  → спаны указывают на .txt
```

Wire the two binaries through the config (`ocr_command`, `ocr_languages`,
`tessdata_prefix`), and resolve them by **override env → PATH → known install
locations**. A path hard-coded to a user profile breaks silently the next time the
account is renamed or the package is upgraded, and every scan then fails with a file-not-
found error that says nothing about OCR.

Trigger the fallback on *yield*, not on file extension: a PDF whose text layer returns
under ~50 characters is a scan regardless of what it claims to be.

### Homoglyph contamination — normalise before you extract anything

Tesseract run as `rus+eng` picks the **Latin twin** of visually identical glyphs, and it
switches alphabet per word rather than per character. The text still reads correctly to a
human and the character count looks healthy, but every rule-based extractor over it
misses matches: `ГК РФ` written with a Latin `K` is simply a different string.

Measured on a 36-page scanned construction contract (2026-07-21): 95 of 12 342 words
(0.77%) came back rendered entirely in Latin twins — `MK` for `ГК`, `CPOKOB`, `3akase`.
Normalising them back recovered a statutory reference the deterministic pass had dropped.

```python
TWINS = str.maketrans("AaBCEeHKMOoPpTXxy", "АаВСЕеНКМОоРрТХху")
text = text.translate(TWINS)   # before regexes, before offsets
```

Do not measure contamination as "words mixing two alphabets" — that metric reports 0.00%
on a visibly contaminated document, because the substitution happens at word granularity.
Measure the share of words composed **entirely** of Latin twins.

Normalisation runs **before** offsets are taken, like every other cleaning step: re-running
OCR later changes the text and invalidates every span already recorded.

### When a scan has a text-layer twin, spans go to the twin

Signed originals often exist twice in a contour: the scan of the executed copy and a
`.docx`/`.pdf` of the draft or working version of the same text. Anchor spans to the
text-layer file and use the OCR only for what **only** the signed copy carries —
signatures, stamps, handwritten inserts, registration marks.

Establish that the two really are the same text deterministically — matching file names
prove nothing. On the pair measured above: 99.4% of the draft's distinct words were
present in the OCR of the scan, and 10 of 12 sampled 15-word fragments matched verbatim.

## Verify the yield — in both directions

Record the extraction method and character count. Then sanity-check:

- A 13-slide deck yielding ~200 characters means extraction failed, not that the deck is
  empty. Re-run with OCR before concluding anything.
- A scan yielding fluent text with no artifacts may mean the OCR silently fell back to
  a text layer that belongs to a different page.
- Compare the character count against the visible size of the document.

Under-extraction is the failure everyone checks for. **Over-extraction is the one that
gets missed:** OCR reads stamps, tables, footers and signature blocks as body text and
manufactures data that was never asserted. On the same contract pair, the deterministic
pass found 7 distinct percentage values in the text layer and 17 in the OCR — ten of them
artefacts (`00%`, `6627%`, `000 %`). A downstream extractor cannot tell those from a
contractual penalty rate.

So: compare counts of rule-extracted entities (sums, percentages, dates, statutory
references) between methods where both are available, and treat a *surplus* as a defect
signal exactly as seriously as a shortfall.

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
