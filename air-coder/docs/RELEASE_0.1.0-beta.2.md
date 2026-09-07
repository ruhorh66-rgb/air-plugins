# AirCoder 0.1.0-beta.2

Release type: prerelease
Release tag: `air-coder--v0.1.0-beta.2`
Release date: 2026-09-07

## Delivered

- Thin executor selector for `ChatGPT + RDC`, `Ruflo`, and native `Codex / Claude Code`.
- Explicit LPR route override.
- Shared result contract for acceptance, elapsed time, direct cost, scarce quota, model class, retries, and evidence.
- Canonical installation from GitHub `ruhorh66-rgb/air-plugins@main` in Claude Code and Codex.
- Windows PowerShell UTF-8 BOM input support.

## Acceptance

- 9/9 unit tests PASS on merged source.
- Claude plugin manifest validation PASS.
- Shared marketplace validation PASS.
- Canonical Codex installed-cache BOM smoke PASS.
- Canonical Claude installed-cache BOM smoke PASS.
- Source / Codex cache / Claude cache selector SHA-256 parity PASS.

## Boundaries

AirCoder is a selector and handoff layer only. It does not own scheduler, watchdog, controller, queue, learning DB, LLM gateway, AirWorker runtime, or Ruflo runtime.
