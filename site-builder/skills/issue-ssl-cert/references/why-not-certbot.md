# Why not certbot / win-acme / Certify The Web?

Any of these is fine **if already installed and working**. This skill exists
for when they aren't, and installing one costs more than the job:

- **certbot**: needs a Python env with the `acme`/`josepy`/`cryptography`
  stack (or a separate install), and on Windows its `--manual` hook flow is
  extra plumbing (auth-hook/cleanup-hook scripts) for a job this small.
- **win-acme**: a real option (single portable exe, no signup) — reach for it
  if you'd rather not touch even minimal ACME protocol code yourself. Not
  bundled here because the ~180-line script is smaller than the download.
- **Certify The Web / Certify Certificate Manager**: a real GUI app, but its
  background service listens on a non-standard loopback address
  (`127.0.0.2`, observed in the wild) which some local security tooling
  (VPN kill-switches with WFP-level filtering) treats as non-local and
  blocks — the UI then shows a stale "service not started" error that
  survives restarts. Diagnosable, but not worth fighting for a one-off cert.
- **ZeroSSL / SSL For Free (free web wizards)**: historically offered a
  no-signup flow; as of this writing both gate the final certificate
  download behind account registration. Creating accounts on the user's
  behalf is out of scope for an agent — don't.

None of these are "wrong" — pick whichever is already frictionless on the
machine you're on. This skill is the fallback when none of them are.
