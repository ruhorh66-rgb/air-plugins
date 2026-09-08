#!/usr/bin/env node
/**
 * gates.mjs
 *
 * Single entry point for site-builder's quality gates (plan section 2.1,
 * docs/PLAN_0_3_0_GATES_AND_SUBAGENTS.md). Runs every check against an
 * already-running built site and writes a machine report (GATES.json) plus
 * a human report (GATES.md). No step calls an LLM: every number in the
 * report comes directly from the underlying tool's own output.
 *
 * Usage:
 *   node scripts/gates.mjs --url http://localhost:3000 --out ./
 *
 * Run `node scripts/gates.mjs --help` for the full option list.
 *
 * Ground rules this file is built around (see the plan doc, non-negotiable):
 *  - a missing tool is reported as status "not_run" with a reason, never a
 *    silent skip and never a crash of the whole run;
 *  - every gate section carries status (pass/fail/not_run), numbers where
 *    applicable, the exact command run, and a duration;
 *  - exit code is 1 if any gate is "fail"; 0 if every gate is "pass"; a
 *    "not_run" does not flip the exit code, but is called out prominently
 *    in both GATES.md and the console output - a false green is worse than
 *    a red, so "not_run" is never silently folded into "pass";
 *  - whenever a tool DID run but its output could not be parsed with
 *    confidence, the gate is marked "fail" (with the raw output attached),
 *    never "pass" - an unreadable result is not evidence of success.
 */

import { mkdir, writeFile, readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { runKeyboardFocusGate } from './keyboard-focus.mjs';

// Каталог самого скрипта: рядом лежат вспомогательные проверки, и путь к ним
// не должен зависеть от того, откуда запущен прогон.
const scriptDir = path.dirname(fileURLToPath(import.meta.url));

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------

const DEFAULT_TIMEOUTS = {
  timeoutAxe: 120_000,
  timeoutPa11y: 120_000,
  timeoutLighthouse: 180_000,
  timeoutLychee: 60_000,
  timeoutSdtt: 60_000,
  timeoutXmllint: 30_000,
  timeoutKeyboardFocus: 60_000,
};

function parseArgs(argv) {
  const args = { out: './', schemas: 'JobPosting', ...DEFAULT_TIMEOUTS };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--url') {
      args.url = argv[++i];
    } else if (a === '--out') {
      args.out = argv[++i];
    } else if (a === '--schemas') {
      args.schemas = argv[++i];
    } else if (a === '--help' || a === '-h') {
      args.help = true;
    } else if (a.startsWith('--timeout-')) {
      const key = a.slice('--timeout-'.length);
      const camel =
        'timeout' + key.split('-').map((p) => p.charAt(0).toUpperCase() + p.slice(1)).join('');
      if (camel in args) args[camel] = Number(argv[++i]);
    }
  }
  return args;
}

function printHelp() {
  console.log(`gates.mjs - deterministic quality gates for a built site

Usage:
  node scripts/gates.mjs --url <url> [--out ./] [options]

Required:
  --url <url>                   URL of the running site to test (e.g. http://localhost:3000)

Options:
  --out <dir>                   Output directory for GATES.json / GATES.md (default: ./)
  --schemas <list>               Schema.org type(s) checked via structured-data-testing-tool
                                 (default: JobPosting; comma-separate for more than one)
  --timeout-axe <ms>             axe-core CLI timeout (default 120000)
  --timeout-pa11y <ms>           pa11y timeout (default 120000)
  --timeout-lighthouse <ms>      Lighthouse CI / Unlighthouse timeout (default 180000)
  --timeout-lychee <ms>          lychee link-check timeout (default 60000)
  --timeout-sdtt <ms>            structured-data-testing-tool timeout (default 60000)
  --timeout-xmllint <ms>         xmllint timeout (default 30000)
  --timeout-keyboard-focus <ms>  Playwright keyboard-focus timeout (default 60000)
  --help, -h                     Show this help and exit

Gates run (each becomes a section of GATES.json / GATES.md):
  axe             axe-core CLI                    WCAG 2.0/2.1/2.2 A/AA rules
  pa11y           pa11y                            second accessibility engine, WCAG2AA
  lighthouse      Lighthouse CI, falls back to     LCP / CLS / TBT (lab proxy for INP),
                  Unlighthouse if lhci can't run    category scores, mobile profile
  linkCheck       lychee                           broken links (binary on PATH, no npm package)
  structuredData  structured-data-testing-tool     schema.org markup (JobPosting by default)
  sitemap         xmllint                          validates sitemap.xml if the site serves one
                                                    (binary on PATH, ships with libxml2)
  keyboardFocus   Playwright (scripts/keyboard-focus.mjs)
                                                    Tab-order focus visibility - not covered by
                                                    axe or pa11y

Every gate reports one of: pass, fail, not_run.
  pass     - the tool ran and found no problem against this script's threshold.
  fail     - the tool ran and found a real problem, OR ran but produced output this
             script could not parse with confidence (treated as fail, never as pass).
  not_run  - the tool/dependency is missing, or the check timed out / hit a network
             error before it could run at all. NOT the same as pass - see GATES.md
             for the exact install command for whatever is missing.

Exit code: 1 if any gate is "fail", 0 otherwise. A "not_run" alone does not fail the
run, but is highlighted with a warning banner in GATES.md and on stderr.

This script makes no calls to any LLM. Every result comes directly from the
underlying tool's own output.
`);
}

// ---------------------------------------------------------------------------
// process helpers
// ---------------------------------------------------------------------------

function quoteArg(value) {
  const s = String(value);
  if (s === '' || /[\s"]/.test(s)) return `"${s.replace(/"/g, '\\"')}"`;
  return s;
}

function buildCommandString(cmd, args) {
  return [cmd, ...args].map(quoteArg).join(' ');
}

function tail(str, n = 4000) {
  if (!str) return '';
  return str.length > n ? `…${str.slice(-n)}` : str;
}

/** Runs a full command line through the platform shell and always resolves
 * (never rejects) with a uniform result shape, so a single gate's process
 * failure can never crash the overall run. */
function runCommand(commandLine, { cwd, timeoutMs = 120_000 } = {}) {
  return new Promise((resolve) => {
    const startedAt = Date.now();
    let stdout = '';
    let stderr = '';
    let timedOut = false;
    let child;

    try {
      child = spawn(commandLine, [], { cwd, shell: true, windowsHide: true });
    } catch (err) {
      resolve({ code: null, signal: null, stdout: '', stderr: '', durationMs: Date.now() - startedAt, timedOut: false, spawnError: err });
      return;
    }

    const timer = setTimeout(() => {
      timedOut = true;
      child.kill('SIGKILL');
    }, timeoutMs);

    child.stdout?.on('data', (chunk) => { stdout += chunk.toString(); });
    child.stderr?.on('data', (chunk) => { stderr += chunk.toString(); });

    child.on('error', (err) => {
      clearTimeout(timer);
      resolve({ code: null, signal: null, stdout, stderr, durationMs: Date.now() - startedAt, timedOut, spawnError: err });
    });

    child.on('close', (code, signal) => {
      clearTimeout(timer);
      resolve({ code, signal, stdout, stderr, durationMs: Date.now() - startedAt, timedOut, spawnError: null });
    });
  });
}

async function runNpxTool(pkgArgs, { cwd, timeoutMs }) {
  const command = buildCommandString('npx', ['--yes', ...pkgArgs]);
  const res = await runCommand(command, { cwd, timeoutMs });
  return { command, res };
}

async function checkBinaryOnPath(bin, { timeoutMs = 8000 } = {}) {
  const command = buildCommandString(bin, ['--version']);
  const res = await runCommand(command, { timeoutMs });
  return { available: !res.spawnError && res.code === 0, command, res };
}

// Patterns that mean "the tool itself never really ran" (missing binary,
// npx couldn't resolve/install the package, no network, etc.) as opposed to
// "the tool ran and found problems" or "the tool ran and crashed on this page".
const MISSING_TOOL_PATTERNS = [
  /command not found/i,
  /is not recognized as an internal or external command/i,
  /\bENOENT\b/,
  /\bENOTFOUND\b/,
  /getaddrinfo/i,
  /\bETIMEDOUT\b/,
  /\bEAI_AGAIN\b/,
  /could not determine executable to run/i,
  /npm error code E404/i,
  /404 Not Found/i,
  /no matching version found/i,
];

const MISSING_RUNTIME_DEP_PATTERNS = [
  /chromedriver/i,
  /session not created/i,
  /unable to discover open window/i,
  /could not find (?:google )?chrome/i,
  /failed to launch (?:the )?browser/i,
  /Executable doesn't exist/i, // playwright/puppeteer browser not installed
  /error while loading shared libraries/i,
];

function firstMatchingLine(text, re) {
  return (text || '').split(/\r?\n/).find((l) => re.test(l)) ?? null;
}

/** Classifies a finished (non-timeout, non-spawn-error) command result as
 * "tool/dependency missing" vs. "tool actually ran". Returns { missing, reason }. */
function classifyFailure(res) {
  const text = `${res.stderr || ''}\n${res.stdout || ''}`;
  for (const re of MISSING_TOOL_PATTERNS) {
    const line = firstMatchingLine(text, re);
    if (line) return { missing: true, reason: `tool/package not available: "${line.trim().slice(0, 300)}"` };
  }
  for (const re of MISSING_RUNTIME_DEP_PATTERNS) {
    const line = firstMatchingLine(text, re);
    if (line) return { missing: true, reason: `runtime dependency missing: "${line.trim().slice(0, 300)}"` };
  }
  return { missing: false, reason: null };
}

// ---------------------------------------------------------------------------
// gate: axe-core CLI (accessibility, WCAG 2.x A/AA)
// ---------------------------------------------------------------------------

async function runAxeGate({ tmpDir, url, timeoutMs }) {
  const { command, res } = await runNpxTool(
    ['@axe-core/cli', url, '--tags', 'wcag2a,wcag2aa,wcag21aa,wcag22aa', '--stdout', '--exit'],
    { cwd: tmpDir, timeoutMs },
  );
  const base = { tool: '@axe-core/cli', command, durationMs: res.durationMs, exitCode: res.code };

  if (res.spawnError) return { ...base, status: 'not_run', reason: `failed to start: ${res.spawnError.message}` };
  if (res.timedOut) return { ...base, status: 'not_run', reason: `timed out after ${timeoutMs}ms` };

  let parsed = null;
  try { parsed = JSON.parse(res.stdout.trim()); } catch { /* not JSON */ }

  if (!parsed) {
    const cls = classifyFailure(res);
    if (cls.missing) {
      return {
        ...base,
        status: 'not_run',
        reason: `${cls.reason}. axe-core CLI also needs a Chromedriver on PATH (npm i -g browser-driver-manager && npx browser-driver-manager install chrome).`,
      };
    }
    return { ...base, status: 'fail', reason: 'axe-core CLI produced no parseable JSON on stdout', stderrTail: tail(res.stderr) };
  }

  const entries = Array.isArray(parsed) ? parsed : [parsed];
  const counts = entries.reduce(
    (acc, e) => {
      acc.violations += (e.violations || []).length;
      acc.passes += (e.passes || []).length;
      acc.incomplete += (e.incomplete || []).length;
      acc.inapplicable += (e.inapplicable || []).length;
      return acc;
    },
    { violations: 0, passes: 0, incomplete: 0, inapplicable: 0 },
  );

  return {
    ...base,
    status: counts.violations > 0 ? 'fail' : 'pass',
    counts,
    violations: entries.flatMap((e) =>
      (e.violations || []).map((v) => ({ id: v.id, impact: v.impact, help: v.help, nodes: (v.nodes || []).length })),
    ),
  };
}

// ---------------------------------------------------------------------------
// gate: pa11y (accessibility, second engine)
// ---------------------------------------------------------------------------

async function runPa11yGate({ tmpDir, url, timeoutMs }) {
  const { command, res } = await runNpxTool(
    ['pa11y', url, '--standard', 'WCAG2AA', '--reporter', 'json'],
    { cwd: tmpDir, timeoutMs },
  );
  const base = { tool: 'pa11y', command, durationMs: res.durationMs, exitCode: res.code };

  if (res.spawnError) return { ...base, status: 'not_run', reason: `failed to start: ${res.spawnError.message}` };
  if (res.timedOut) return { ...base, status: 'not_run', reason: `timed out after ${timeoutMs}ms` };

  let parsed = null;
  try { parsed = JSON.parse(res.stdout.trim()); } catch { /* not JSON */ }

  if (!parsed) {
    const cls = classifyFailure(res);
    if (cls.missing) return { ...base, status: 'not_run', reason: cls.reason };
    return { ...base, status: 'fail', reason: 'pa11y produced no parseable JSON on stdout', stderrTail: tail(res.stderr) };
  }

  const issues = Array.isArray(parsed) ? parsed : parsed.issues || [];
  const counts = issues.reduce(
    (acc, issue) => {
      const t = String(issue.type || 'unknown').toLowerCase();
      acc[t] = (acc[t] || 0) + 1;
      acc.total += 1;
      return acc;
    },
    { total: 0 },
  );

  return {
    ...base,
    status: (counts.error || 0) > 0 ? 'fail' : 'pass',
    counts,
    issues: issues.slice(0, 50).map((i) => ({ type: i.type, code: i.code, message: i.message, selector: i.selector })),
  };
}

// ---------------------------------------------------------------------------
// gate: Lighthouse CI, falling back to Unlighthouse (LCP / CLS / TBT-as-INP-proxy)
// ---------------------------------------------------------------------------

// Official Core Web Vitals "poor" boundaries (web.dev/vitals). Crossing one
// of these fails the gate; INP itself is not measurable in a lab Lighthouse
// run without simulated interaction, so Total Blocking Time is used as the
// closest lab proxy - this substitution is deliberate and documented, not a
// silent approximation.
const CWV_THRESHOLDS = {
  lcpMsPoor: 4000,
  clsPoor: 0.25,
  tbtMsPoor: 600,
  performanceScorePoor: 0.5,
};

function buildLighthouseResult({ tool, command, durationMs, exitCode, metrics }) {
  const allNull = ['performanceScore', 'cls', 'lcpMs', 'tbtMs'].every((k) => metrics[k] == null);
  if (allNull) {
    return {
      tool,
      command,
      durationMs,
      exitCode,
      status: 'fail',
      reason: 'a report file was produced but no usable performance metric could be extracted from it',
      metrics,
    };
  }

  const reasons = [];
  if (metrics.lcpMs != null && metrics.lcpMs > CWV_THRESHOLDS.lcpMsPoor) {
    reasons.push(`LCP ${Math.round(metrics.lcpMs)}ms > ${CWV_THRESHOLDS.lcpMsPoor}ms`);
  }
  if (metrics.cls != null && metrics.cls > CWV_THRESHOLDS.clsPoor) {
    reasons.push(`CLS ${metrics.cls} > ${CWV_THRESHOLDS.clsPoor}`);
  }
  if (metrics.tbtMs != null && metrics.tbtMs > CWV_THRESHOLDS.tbtMsPoor) {
    reasons.push(`TBT ${Math.round(metrics.tbtMs)}ms > ${CWV_THRESHOLDS.tbtMsPoor}ms (lab proxy for INP)`);
  }
  if (metrics.performanceScore != null && metrics.performanceScore < CWV_THRESHOLDS.performanceScorePoor) {
    reasons.push(`performance score ${metrics.performanceScore} < ${CWV_THRESHOLDS.performanceScorePoor}`);
  }

  return {
    tool,
    command,
    durationMs,
    exitCode,
    status: reasons.length > 0 ? 'fail' : 'pass',
    reason: reasons.length > 0 ? reasons.join('; ') : undefined,
    metrics,
    thresholds: CWV_THRESHOLDS,
    note: 'INP is not measurable in a lab Lighthouse run; Total Blocking Time (TBT) is used as the closest lab proxy.',
  };
}

async function tryParseLhciManifest(lhciOutDir) {
  try {
    const manifest = JSON.parse(await readFile(path.join(lhciOutDir, 'manifest.json'), 'utf8'));
    const entries = Array.isArray(manifest) ? manifest : [manifest];
    const rep = entries.find((e) => e.isRepresentativeRun) || entries[0];
    if (!rep) return null;
    const summary = rep.summary || {};
    let lcpMs = null;
    let cls = null;
    let tbtMs = null;
    if (rep.jsonPath) {
      try {
        const lhr = JSON.parse(await readFile(rep.jsonPath, 'utf8'));
        const audits = lhr.audits || {};
        lcpMs = audits['largest-contentful-paint']?.numericValue ?? null;
        cls = audits['cumulative-layout-shift']?.numericValue ?? null;
        tbtMs = audits['total-blocking-time']?.numericValue ?? null;
      } catch { /* fall back to summary-only scores below */ }
    }
    return {
      performanceScore: summary.performance ?? null,
      accessibilityScore: summary.accessibility ?? null,
      bestPracticesScore: summary['best-practices'] ?? null,
      seoScore: summary.seo ?? null,
      lcpMs,
      cls,
      tbtMs,
    };
  } catch {
    return null;
  }
}

async function findJsonFilesRecursive(dir, depth) {
  if (depth < 0) return [];
  let entries;
  try {
    entries = await readdir(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  let out = [];
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) out = out.concat(await findJsonFilesRecursive(full, depth - 1));
    else if (entry.name.endsWith('.json')) out.push(full);
  }
  return out;
}

async function tryParseUnlighthouseReport(dir) {
  const files = await findJsonFilesRecursive(dir, 4);
  for (const file of files) {
    try {
      const data = JSON.parse(await readFile(file, 'utf8'));
      const candidate = Array.isArray(data) ? data[0] : data;
      const report = candidate?.report ?? candidate;
      const categories = report?.categories;
      if (categories?.performance) {
        const audits = report.audits || {};
        return {
          performanceScore: categories.performance.score ?? null,
          accessibilityScore: categories.accessibility?.score ?? null,
          bestPracticesScore: categories['best-practices']?.score ?? null,
          seoScore: categories.seo?.score ?? null,
          lcpMs: audits['largest-contentful-paint']?.numericValue ?? null,
          cls: audits['cumulative-layout-shift']?.numericValue ?? null,
          tbtMs: audits['total-blocking-time']?.numericValue ?? null,
        };
      }
    } catch { /* try next file */ }
  }
  return null;
}

async function runLighthouseGate({ tmpDir, url, timeoutMs }) {
  const lhciOutDir = path.join(tmpDir, 'lhci');
  await mkdir(lhciOutDir, { recursive: true });
  const { command: lhciCommand, res: lhciRes } = await runNpxTool(
    [
      '@lhci/cli',
      'autorun',
      `--collect.url=${url}`,
      '--collect.numberOfRuns=1',
      '--collect.settings.chromeFlags=--headless=new --no-sandbox',
      '--upload.target=filesystem',
      `--upload.outputDir=${lhciOutDir}`,
    ],
    { cwd: tmpDir, timeoutMs },
  );

  const lhciMetrics = await tryParseLhciManifest(lhciOutDir);
  if (lhciMetrics) {
    return buildLighthouseResult({
      tool: '@lhci/cli (lhci autorun)',
      command: lhciCommand,
      durationMs: lhciRes.durationMs,
      exitCode: lhciRes.code,
      metrics: lhciMetrics,
    });
  }

  const clsLhci = classifyFailure(lhciRes);

  // lhci produced nothing usable - fall back to unlighthouse-ci, exactly as
  // the plan lists it as an alternative command for this same gate.
  const unlighthouseDir = path.join(tmpDir, 'unlighthouse');
  await mkdir(unlighthouseDir, { recursive: true });
  const { command: ulhCommand, res: ulhRes } = await runNpxTool(
    ['@unlighthouse/cli', 'unlighthouse-ci', '--site', url, '--mobile'],
    { cwd: unlighthouseDir, timeoutMs },
  );

  const ulhMetrics = await tryParseUnlighthouseReport(unlighthouseDir);
  if (ulhMetrics) {
    return buildLighthouseResult({
      tool: '@unlighthouse/cli (unlighthouse-ci, fallback)',
      command: ulhCommand,
      durationMs: ulhRes.durationMs,
      exitCode: ulhRes.code,
      metrics: ulhMetrics,
    });
  }

  const clsUlh = classifyFailure(ulhRes);
  const bothMissing = clsLhci.missing && clsUlh.missing;

  return {
    tool: '@lhci/cli or @unlighthouse/cli',
    command: `${lhciCommand}  ||  ${ulhCommand}`,
    durationMs: lhciRes.durationMs + ulhRes.durationMs,
    lhciExitCode: lhciRes.code,
    unlighthouseExitCode: ulhRes.code,
    status: bothMissing ? 'not_run' : 'fail',
    reason: bothMissing
      ? `neither tool produced a usable report. lhci: ${clsLhci.reason ?? 'no manifest.json produced'}; unlighthouse-ci: ${clsUlh.reason ?? 'no report produced'}`
      : 'both tools ran but produced no parseable report - treated as fail, not not_run (see stderr tails)',
    lhciStderrTail: tail(lhciRes.stderr),
    unlighthouseStderrTail: tail(ulhRes.stderr),
  };
}

// ---------------------------------------------------------------------------
// gate: lychee (broken links) - a standalone Rust binary, NOT an npm package.
// The "lychee" name on the npm registry is an unrelated tool; there is no
// official npx wrapper for lycheeverse/lychee, so this checks PATH directly.
// ---------------------------------------------------------------------------

async function runLycheeGate({ url, outDir, timeoutMs }) {
  const versionCheck = await checkBinaryOnPath('lychee');
  if (!versionCheck.available) {
    return {
      tool: 'lychee',
      command: versionCheck.command,
      status: 'not_run',
      reason:
        'lychee binary not found on PATH. There is no official npm/npx package for the real ' +
        'link-checker (the "lychee" package on npm is an unrelated tool). Install: ' +
        '`cargo install lychee` (needs Rust), or download a release binary for your platform ' +
        'from https://github.com/lycheeverse/lychee/releases and add it to PATH.',
      durationMs: versionCheck.res.durationMs,
    };
  }

  const command = buildCommandString('lychee', ['--no-progress', '--format', 'json', url]);
  const res = await runCommand(command, { cwd: outDir, timeoutMs });
  const base = { tool: 'lychee', command, durationMs: res.durationMs, exitCode: res.code };

  if (res.timedOut) return { ...base, status: 'not_run', reason: `timed out after ${timeoutMs}ms` };

  let parsed = null;
  try { parsed = JSON.parse(res.stdout.trim()); } catch { /* not JSON */ }

  if (!parsed) {
    return { ...base, status: 'fail', reason: 'lychee produced no parseable JSON on stdout', stderrTail: tail(res.stderr) };
  }

  const total = parsed.total ?? null;
  const successful = parsed.successful ?? null;
  const failures =
    parsed.errors ??
    parsed.failures ??
    (parsed.error_map ? Object.values(parsed.error_map).flat().length : null);

  if (failures == null) {
    return {
      ...base,
      status: 'fail',
      reason: "lychee ran but its JSON shape was not recognized by this script (schema drift) - review the raw output manually",
      raw: parsed,
    };
  }

  return {
    ...base,
    status: failures > 0 ? 'fail' : 'pass',
    counts: { total, successful, failures },
    errorMap: parsed.error_map ?? undefined,
  };
}

// ---------------------------------------------------------------------------
// gate: structured-data-testing-tool (schema.org, incl. JobPosting)
// ---------------------------------------------------------------------------

async function runStructuredDataGate({ tmpDir, url, timeoutMs, schemas }) {
  const outputFile = path.join(tmpDir, 'sdtt.json');
  const { command, res } = await runNpxTool(
    ['structured-data-testing-tool', '--url', url, '--schemas', schemas, '--output', outputFile],
    { cwd: tmpDir, timeoutMs },
  );
  const base = { tool: 'structured-data-testing-tool', command, durationMs: res.durationMs, exitCode: res.code };

  if (res.spawnError) return { ...base, status: 'not_run', reason: `failed to start: ${res.spawnError.message}` };
  if (res.timedOut) return { ...base, status: 'not_run', reason: `timed out after ${timeoutMs}ms` };

  let parsed = null;
  try { parsed = JSON.parse(await readFile(outputFile, 'utf8')); } catch { /* file missing or invalid */ }

  if (!parsed) {
    const cls = classifyFailure(res);
    if (cls.missing) return { ...base, status: 'not_run', reason: cls.reason };
    return {
      ...base,
      status: 'fail',
      reason: `no output file produced at ${outputFile}`,
      stderrTail: tail(res.stderr),
      stdoutTail: tail(res.stdout),
    };
  }

  const passed = parsed.passed || [];
  const failed = parsed.failed || [];
  const warnings = parsed.warnings || [];
  const schemasFound = parsed.schemas || [];

  return {
    ...base,
    status: failed.length > 0 ? 'fail' : 'pass',
    counts: { passed: passed.length, failed: failed.length, warnings: warnings.length },
    requestedSchemas: schemas,
    schemasFound,
  };
}

// ---------------------------------------------------------------------------
// gate: xmllint (sitemap.xml validity, only applies if a sitemap is served)
// xmllint ships with libxml2, not npm - checked on PATH directly.
// ---------------------------------------------------------------------------

async function runSitemapGate({ url, tmpDir, timeoutMs }) {
  const sitemapUrl = new URL('/sitemap.xml', url).toString();
  const startedAt = Date.now();
  let fetchRes;
  try {
    fetchRes = await fetch(sitemapUrl, { signal: AbortSignal.timeout(10_000) });
  } catch (err) {
    return {
      tool: 'xmllint',
      command: `xmllint --noout ${sitemapUrl}`,
      status: 'not_run',
      reason: `could not fetch ${sitemapUrl}: ${err?.message ?? err}`,
      durationMs: Date.now() - startedAt,
    };
  }

  if (!fetchRes.ok) {
    return {
      tool: 'xmllint',
      command: `xmllint --noout ${sitemapUrl}`,
      status: 'not_run',
      reason: `no sitemap.xml found at ${sitemapUrl} (HTTP ${fetchRes.status}) - this gate only applies when a sitemap is served`,
      durationMs: Date.now() - startedAt,
    };
  }

  const body = await fetchRes.text();
  const sitemapPath = path.join(tmpDir, 'sitemap.xml');
  await writeFile(sitemapPath, body, 'utf8');

  const versionCheck = await checkBinaryOnPath('xmllint');
  if (!versionCheck.available) {
    // Fallback to the bundled Python checker instead of reporting not_run.
    // Added 2026-09-08: libxml2 has no winget package, and `xmllint --noout` only
    // checks well-formedness - which the standard library already does. The bundled
    // checker does MORE: sitemaps.org rules (namespace, required <loc>, the 50000
    // entry and 50MB limits) that xmllint does not know about.
    // Reporting not_run while a working checker sits in the same folder would be a
    // gate refusing to run for no reason.
    const pyChecker = path.join(scriptDir, 'sitemap_check.py');
    const python = (await checkBinaryOnPath('python')).available
      ? 'python'
      : ((await checkBinaryOnPath('python3')).available ? 'python3' : null);

    if (!python) {
      return {
        tool: 'sitemap_check.py',
        command: `python ${pyChecker} ${sitemapPath}`,
        status: 'not_run',
        reason: 'neither xmllint nor python found on PATH; install either to run this gate',
        durationMs: Date.now() - startedAt,
        sitemapFetched: true,
        sitemapUrl,
      };
    }

    const pyCommand = buildCommandString(python, [pyChecker, sitemapPath]);
    const pyRes = await runCommand(pyCommand, { timeoutMs });
    let parsed = null;
    try { parsed = JSON.parse((pyRes.stdout || '').trim()); } catch { /* unreadable is not success */ }

    if (!parsed) {
      return {
        tool: 'sitemap_check.py',
        command: pyCommand,
        status: 'fail',
        reason: 'sitemap checker produced output this script cannot parse - unreadable is not success',
        stderr: (pyRes.stderr || '').slice(0, 400),
        durationMs: Date.now() - startedAt,
        sitemapFetched: true,
        sitemapUrl,
      };
    }

    return {
      tool: 'sitemap_check.py',
      command: pyCommand,
      status: parsed.status === 'pass' ? 'pass' : (parsed.status === 'not_run' ? 'not_run' : 'fail'),
      details: parsed,
      note: 'xmllint absent; validated with the bundled checker, which also applies sitemaps.org rules',
      durationMs: Date.now() - startedAt,
      sitemapFetched: true,
      sitemapUrl,
    };
  }

  const command = buildCommandString('xmllint', ['--noout', sitemapPath]);
  const res = await runCommand(command, { timeoutMs });

  return {
    tool: 'xmllint',
    command,
    status: res.code === 0 ? 'pass' : 'fail',
    exitCode: res.code,
    durationMs: Date.now() - startedAt + res.durationMs,
    reason: res.code === 0 ? undefined : tail(res.stderr, 2000),
    sitemapUrl,
    sitemapBytes: body.length,
  };
}

// ---------------------------------------------------------------------------
// report rendering
// ---------------------------------------------------------------------------

function renderMarkdown(report) {
  const lines = [];
  lines.push('# GATES.md — site-builder quality gates');
  lines.push('');
  lines.push(`Generated: ${report.generatedAt}`);
  lines.push(`Target URL: ${report.url}`);
  lines.push('');
  lines.push(
    `**Result: ${report.exitCode === 0 ? 'OK (exit 0)' : 'FAIL (exit 1)'}** — ` +
      `${report.summary.pass} pass / ${report.summary.fail} fail / ${report.summary.not_run} not_run (of ${report.summary.total})`,
  );
  lines.push('');

  if (report.summary.not_run > 0) {
    lines.push(
      '> ⚠ **Some gates did not run.** `not_run` is not a pass — it means this script could ' +
        'not verify that property at all, usually because an external tool or runtime ' +
        'dependency is missing. See the per-gate sections below for the exact install command.',
    );
    lines.push('');
  }

  lines.push('| Gate | Status | Command | Duration |');
  lines.push('|---|---|---|---|');
  for (const [name, g] of Object.entries(report.gates)) {
    const icon = g.status === 'pass' ? '✅ pass' : g.status === 'fail' ? '❌ fail' : '⚠️ not_run';
    const cmd = (g.command || '—').replace(/\|/g, '\\|');
    lines.push(`| ${name} | ${icon} | \`${cmd}\` | ${g.durationMs != null ? `${g.durationMs} ms` : '—'} |`);
  }
  lines.push('');

  for (const [name, g] of Object.entries(report.gates)) {
    lines.push(`## ${name}`);
    lines.push('');
    lines.push(`- status: **${g.status}**`);
    if (g.tool) lines.push(`- tool: ${g.tool}`);
    if (g.command) lines.push(`- command: \`${g.command}\``);
    if (g.durationMs != null) lines.push(`- duration: ${g.durationMs} ms`);
    if (g.exitCode !== undefined) lines.push(`- exit code: ${g.exitCode}`);
    if (g.reason) lines.push(`- reason: ${g.reason}`);
    if (g.counts) lines.push(`- counts: \`${JSON.stringify(g.counts)}\``);
    if (g.metrics) lines.push(`- metrics: \`${JSON.stringify(g.metrics)}\``);
    if (g.note) lines.push(`- note: ${g.note}`);
    if (g.failingElements && g.failingElements.length > 0) {
      lines.push(`- elements without visible focus (${g.failingElements.length}):`);
      for (const el of g.failingElements.slice(0, 20)) {
        lines.push(`  - \`<${el.tag}>\` "${el.text}"`);
      }
    }
    lines.push('');
  }

  lines.push('---');
  lines.push('');
  lines.push('_Generated deterministically by `scripts/gates.mjs`. No LLM was involved in producing these numbers._');
  lines.push('');

  return lines.join('\n');
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

async function main() {
  const args = parseArgs(process.argv.slice(2));

  if (args.help) {
    printHelp();
    process.exitCode = 0;
    return;
  }

  if (!args.url) {
    console.error('Error: --url is required.\n');
    printHelp();
    process.exitCode = 1;
    return;
  }

  const outDir = path.resolve(args.out);
  await mkdir(outDir, { recursive: true });
  const tmpDir = path.join(outDir, '.gates-tmp');
  await mkdir(tmpDir, { recursive: true });

  const generatedAt = new Date().toISOString();
  const gates = {};

  const steps = [
    ['axe', () => runAxeGate({ tmpDir, url: args.url, timeoutMs: args.timeoutAxe })],
    ['pa11y', () => runPa11yGate({ tmpDir, url: args.url, timeoutMs: args.timeoutPa11y })],
    ['lighthouse', () => runLighthouseGate({ tmpDir, url: args.url, timeoutMs: args.timeoutLighthouse })],
    ['linkCheck', () => runLycheeGate({ url: args.url, outDir: tmpDir, timeoutMs: args.timeoutLychee })],
    ['structuredData', () => runStructuredDataGate({ tmpDir, url: args.url, timeoutMs: args.timeoutSdtt, schemas: args.schemas })],
    ['sitemap', () => runSitemapGate({ url: args.url, tmpDir, timeoutMs: args.timeoutXmllint })],
    ['keyboardFocus', () => runKeyboardFocusGate({ url: args.url, timeoutMs: args.timeoutKeyboardFocus })],
  ];

  for (const [name, fn] of steps) {
    console.log(`[gates] running ${name}...`);
    try {
      // eslint-disable-next-line no-await-in-loop
      gates[name] = await fn();
    } catch (err) {
      // A gate must never be able to crash the whole run - any uncaught
      // throw becomes a visible not_run instead of an aborted process.
      gates[name] = { status: 'not_run', reason: `gate crashed unexpectedly: ${err?.stack ?? err}` };
    }
    console.log(`[gates] ${name}: ${gates[name].status}${gates[name].reason ? ' - ' + gates[name].reason : ''}`);
  }

  const summary = { pass: 0, fail: 0, not_run: 0, total: 0 };
  for (const g of Object.values(gates)) {
    summary.total += 1;
    summary[g.status] = (summary[g.status] || 0) + 1;
  }

  const exitCode = summary.fail > 0 ? 1 : 0;

  const report = { generatedAt, url: args.url, gates, summary, exitCode };

  await writeFile(path.join(outDir, 'GATES.json'), JSON.stringify(report, null, 2), 'utf8');
  await writeFile(path.join(outDir, 'GATES.md'), renderMarkdown(report), 'utf8');

  console.log('');
  console.log(`[gates] summary: ${summary.pass} pass, ${summary.fail} fail, ${summary.not_run} not_run (of ${summary.total})`);
  if (summary.not_run > 0) {
    console.warn(
      `[gates] WARNING: ${summary.not_run} gate(s) could not run - see GATES.md for install instructions. This is NOT the same as passing.`,
    );
  }
  console.log(`[gates] reports written: ${path.join(outDir, 'GATES.json')}, ${path.join(outDir, 'GATES.md')}`);

  process.exitCode = exitCode;
}

main().catch((err) => {
  console.error('[gates] fatal error:', err);
  process.exitCode = 1;
});
