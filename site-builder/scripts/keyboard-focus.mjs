#!/usr/bin/env node
/**
 * keyboard-focus.mjs
 *
 * Closes a gate axe-core and pa11y do NOT close: tabbing through a page's
 * interactive elements and confirming that keyboard focus is *visible* —
 * an outline or box-shadow/ring that differs from the element's own
 * unfocused state. axe/pa11y check DOM structure and ARIA; neither of them
 * drives real keyboard input or compares before/after focus styles.
 *
 * Normal usage is programmatic: `scripts/gates.mjs` imports
 * `runKeyboardFocusGate()` and merges its result into GATES.json as the
 * "keyboardFocus" section.
 *
 * Standalone usage (for manual debugging of this gate only):
 *   node scripts/keyboard-focus.mjs --url http://localhost:3000 [--out ./] \
 *        [--timeout 60000] [--max-elements 150]
 *
 * If Playwright is not installed (or has no Chromium binary), this
 * resolves to { status: 'not_run', reason: ... } instead of throwing —
 * a missing dev dependency must never crash the whole gate run.
 *
 * Known limitations (documented, not silently ignored):
 *  - only real DOM tab stops matched by FOCUSABLE_SELECTOR are checked;
 *    elements inside same-origin iframes or open shadow roots are not
 *    traversed into.
 *  - "visible focus" is approximated as "outline changed and is not
 *    none/0px" OR "box-shadow changed and is not none". This matches how
 *    both native `:focus-visible` outlines and Tailwind `focus:ring-*`
 *    utilities render, but a focus style implemented purely via a color
 *    change with no outline/shadow (e.g. text-color-only) is not detected.
 */

import { writeFile, mkdir } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const DEFAULT_TIMEOUT_MS = 60_000;
const DEFAULT_MAX_ELEMENTS = 150;

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
  '[contenteditable="true"]',
].join(', ');

/**
 * Runs the keyboard-focus-visibility gate against a live URL.
 * @returns {Promise<object>} a GATES.json-shaped section: { status, reason?,
 *   command, durationMs, counts?, failingElements?, error? }
 */
export async function runKeyboardFocusGate({
  url,
  timeoutMs = DEFAULT_TIMEOUT_MS,
  maxElements = DEFAULT_MAX_ELEMENTS,
} = {}) {
  const startedAt = Date.now();
  const command = `node scripts/keyboard-focus.mjs --url ${url ?? '<missing>'}`;

  if (!url) {
    return { status: 'not_run', reason: 'no url provided', command, durationMs: 0 };
  }

  let playwright;
  try {
    playwright = await import('playwright');
  } catch (err) {
    return {
      status: 'not_run',
      reason:
        'playwright is not installed. Install it with: npm i -D playwright && npx playwright install chromium',
      command,
      durationMs: Date.now() - startedAt,
      error: String(err?.message ?? err),
    };
  }

  let browser;
  try {
    browser = await playwright.chromium.launch({ headless: true });
  } catch (err) {
    return {
      status: 'not_run',
      reason:
        'playwright is installed but no Chromium binary is available. Run: npx playwright install chromium',
      command,
      durationMs: Date.now() - startedAt,
      error: String(err?.message ?? err),
    };
  }

  try {
    const context = await browser.newContext();
    const page = await context.newPage();
    await page.goto(url, { waitUntil: 'load', timeout: timeoutMs });

    // Tag every visible, focusable element with a stable index and record
    // its *unfocused* computed style as a baseline, all inside one
    // evaluate() call so the DOM snapshot is internally consistent.
    const candidateCount = await page.evaluate(
      ({ selector, limit }) => {
        const isVisible = (el) => {
          const rect = el.getBoundingClientRect();
          const style = window.getComputedStyle(el);
          return (
            rect.width > 0 &&
            rect.height > 0 &&
            style.visibility !== 'hidden' &&
            style.display !== 'none'
          );
        };
        const nodes = Array.from(document.querySelectorAll(selector))
          .filter(isVisible)
          .slice(0, limit);
        window.__kfBaseline = [];
        nodes.forEach((el, idx) => {
          el.setAttribute('data-kf-idx', String(idx));
          const cs = window.getComputedStyle(el);
          window.__kfBaseline.push({
            idx,
            tag: el.tagName.toLowerCase(),
            text: (el.textContent || el.getAttribute('aria-label') || el.getAttribute('alt') || '')
              .trim()
              .slice(0, 60),
            outlineStyle: cs.outlineStyle,
            outlineWidth: cs.outlineWidth,
            outlineColor: cs.outlineColor,
            boxShadow: cs.boxShadow,
          });
        });
        return nodes.length;
      },
      { selector: FOCUSABLE_SELECTOR, limit: maxElements },
    );

    if (candidateCount === 0) {
      await context.close();
      return {
        status: 'not_run',
        reason: 'no visible focusable elements found on the page',
        command,
        durationMs: Date.now() - startedAt,
      };
    }

    await page.evaluate(() => document.body && document.body.focus());

    const visited = new Set();
    const failing = [];
    const maxPresses = candidateCount * 3 + 20;
    let stagnantPresses = 0;

    for (let i = 0; i < maxPresses && visited.size < candidateCount && stagnantPresses < 40; i++) {
      await page.keyboard.press('Tab');
      // eslint-disable-next-line no-await-in-loop
      const focused = await page.evaluate(() => {
        const active = document.activeElement;
        if (!active || !active.hasAttribute('data-kf-idx')) return null;
        const idx = Number(active.getAttribute('data-kf-idx'));
        const cs = window.getComputedStyle(active);
        return {
          idx,
          outlineStyle: cs.outlineStyle,
          outlineWidth: cs.outlineWidth,
          outlineColor: cs.outlineColor,
          boxShadow: cs.boxShadow,
        };
      });

      if (!focused || visited.has(focused.idx)) {
        stagnantPresses += 1;
        continue;
      }
      stagnantPresses = 0;
      visited.add(focused.idx);

      // eslint-disable-next-line no-await-in-loop
      const baseline = await page.evaluate(
        (idx) => (window.__kfBaseline || []).find((b) => b.idx === idx) ?? null,
        focused.idx,
      );
      if (!baseline) continue;

      const outlineChanged =
        focused.outlineStyle !== 'none' &&
        focused.outlineWidth !== '0px' &&
        (focused.outlineStyle !== baseline.outlineStyle ||
          focused.outlineWidth !== baseline.outlineWidth ||
          focused.outlineColor !== baseline.outlineColor);
      const boxShadowChanged = focused.boxShadow !== 'none' && focused.boxShadow !== baseline.boxShadow;
      const hasVisibleFocus = outlineChanged || boxShadowChanged;

      if (!hasVisibleFocus) {
        failing.push({ idx: baseline.idx, tag: baseline.tag, text: baseline.text });
      }
    }

    await context.close();

    const checked = visited.size;
    // Ran but the tab loop never landed on a tagged element at all: don't
    // silently call that a pass with 0 checked - flag it as not_run so it
    // is visible in the report instead of inflating the pass count.
    const status = checked === 0 ? 'not_run' : failing.length > 0 ? 'fail' : 'pass';

    return {
      status,
      reason:
        status === 'not_run'
          ? 'tab traversal never landed on a tagged element (possible focus trap or non-standard tab handling)'
          : undefined,
      command,
      durationMs: Date.now() - startedAt,
      counts: {
        candidates: candidateCount,
        checked,
        withoutVisibleFocus: failing.length,
      },
      failingElements: failing,
    };
  } catch (err) {
    // Never let an unexpected page/runtime error crash the whole gates.mjs
    // run - degrade to a reported failure with the error attached.
    return {
      status: 'fail',
      reason: 'keyboard-focus check threw an unexpected error while running',
      command,
      durationMs: Date.now() - startedAt,
      error: String(err?.stack ?? err),
    };
  } finally {
    await browser.close().catch(() => {});
  }
}

function parseArgs(argv) {
  const args = { out: './', timeout: DEFAULT_TIMEOUT_MS, maxElements: DEFAULT_MAX_ELEMENTS };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--url') args.url = argv[++i];
    else if (a === '--out') args.out = argv[++i];
    else if (a === '--timeout') args.timeout = Number(argv[++i]);
    else if (a === '--max-elements') args.maxElements = Number(argv[++i]);
    else if (a === '--help' || a === '-h') args.help = true;
  }
  return args;
}

function printHelp() {
  console.log(`keyboard-focus.mjs - standalone runner for the keyboard-focus-visibility gate

Usage:
  node scripts/keyboard-focus.mjs --url <url> [--out ./] [--timeout 60000] [--max-elements 150]

This gate is normally run in-process by scripts/gates.mjs and merged into
GATES.json as the "keyboardFocus" section. Run this file directly only to
debug the gate in isolation; it writes its own keyboard-focus.json into --out.
`);
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help || !args.url) {
    printHelp();
    process.exitCode = args.help ? 0 : 1;
    return;
  }
  const result = await runKeyboardFocusGate({
    url: args.url,
    timeoutMs: args.timeout,
    maxElements: args.maxElements,
  });
  const outDir = path.resolve(args.out);
  await mkdir(outDir, { recursive: true });
  const outPath = path.join(outDir, 'keyboard-focus.json');
  await writeFile(outPath, JSON.stringify(result, null, 2), 'utf8');
  console.log(JSON.stringify(result, null, 2));
  console.log(`\nWritten: ${outPath}`);
  process.exitCode = result.status === 'fail' ? 1 : 0;
}

const isMainModule = (() => {
  try {
    return import.meta.url === pathToFileURL(process.argv[1] ?? '').href;
  } catch {
    return false;
  }
})();

if (isMainModule) {
  main().catch((err) => {
    console.error('keyboard-focus.mjs crashed:', err);
    process.exitCode = 1;
  });
}
