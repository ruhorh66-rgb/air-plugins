---
name: issue-ssl-cert
description: Issue a free multi-domain Let's Encrypt SSL certificate via a minimal self-contained ACME v2 (HTTP-01) client, when no CLI ACME client (certbot/win-acme) is installed and free web wizards (ZeroSSL/SSL For Free) require account signup. Use when a site's HTTPS fails with a certificate/hostname-mismatch error and the domain's webroot is reachable over FTP/SFTP.
---

# Issue SSL Cert

Get a real, trusted (Let's Encrypt) SSL certificate covering one or more domains,
with **no account signup anywhere**, **no CLI tool install**, and **no long-lived
secrets on disk** — just the domain's own document root and one Python run.

## When to use this

- A site's HTTPS shows `NET::ERR_CERT_COMMON_NAME_INVALID` / `SEC_E_WRONG_PRINCIPAL`
  because the installed certificate's SAN list doesn't cover a domain/alias you just
  added (e.g. a new redirect domain pointed at an existing site's docroot).
- You need a cert for multiple domains/aliases sharing one webroot.
- `certbot`/`win-acme`/`Certify The Web` aren't installed and installing them is
  more friction than the job needs (service/UAC/port issues, GUI automation
  fights local security tooling) — see `references/why-not-certbot.md`.
- ZeroSSL / SSL For Free's free wizards now gate the final step behind account
  creation — you should not create accounts on the user's behalf.

If a working ACME client is already installed and reachable, prefer it — this
skill exists for the "nothing works, need it now" case.

## How it works

`scripts/get_cert.py` is a ~180-line, dependency-free ACME v2 client: stdlib
(`urllib`, `json`, `base64`, `hashlib`) + the system `openssl` binary for
keys/signing/CSR. No `acme`/`certbot`/`cryptography`-library dependency.

1. Generates an ACME account key + a fresh domain key + CSR (SAN = all domains).
2. Talks directly to `https://acme-v02.api.letsencrypt.org/directory` — registers
   an account (no email required to work, though supplying one is polite),
   opens an order for all domains.
3. For each domain's HTTP-01 challenge: uploads the challenge token to
   `<docroot>/.well-known/acme-challenge/<token>` via **FTP** (`ftplib`,
   stdlib), then asks ACME to validate.
4. Finalizes the order, downloads the fullchain certificate.
5. Copies `fullchain.pem` + the private key to the Desktop so they can be
   pasted/uploaded through the hosting panel's own "install certificate"
   form (paste-cert-and-key, no auto-issue button — typical of ISPmanager-
   style panels like RU-CENTER's `hcp2`).

**Gotcha confirmed 04.08.2026 (RU-CENTER `hcp2`):** uploading the certificate
is not the last step. The panel can list the new cert while some domain
aliases still serve the *old* one over TLS (SNI-based vhosts each keep their
own binding) — after upload, open the SSL page for the site and explicitly
enable/select the new certificate for **every** domain/alias it should cover
(a per-domain toggle list, not implied by "uploaded"). Verify per-domain
after this, not just once for the primary hostname — see
`references/verify-hostname-match.md`.

## Preconditions

- `openssl` on PATH (ships with Git for Windows / most dev machines).
- FTP (or adapt for SFTP) write access to the **document root** that the
  target domain(s) actually serve — same root for all domains in one run.
- Each target domain must resolve (A record) to that server and respond on
  plain HTTP port 80 at `/.well-known/acme-challenge/...` **without being
  redirected away** (a domain that 301-redirects everything, e.g. a
  redirect-only alias, needs a one-line exception added to its rewrite rules
  first — see `references/htaccess-exception.md`).

## Usage

```
python scripts/get_cert.py \
  --domain tech-77.com --domain www.tech-77.com \
  --domain xn--e1azq.xn--p1ai --domain www.xn--e1azq.xn--p1ai \
  --ftp-host ftp.example-hosting.ru --ftp-user myaccount_ftp \
  --docroot /example.com/docs \
  --out-dir "%USERPROFILE%\Desktop"
```

It prompts for the FTP password **at runtime, visibly** (`input()`, not
`getpass`) — this project's standing rule is visible password entry to avoid
mistyped-hidden-password round-trips (see `secrets-never-in-chat` memory
convention). **Never pass the password as a CLI argument or hardcode it in
the script** — a prior version of this exact script had a real password
hardcoded in its source, which then persisted in plaintext inside a saved
session transcript. Runtime prompt only.

After it prints `DONE`, upload the two Desktop files through the hosting
panel's certificate-install page — that step stays manual (it's a one-time
paste, not worth automating for an occasional operation).

## Verifying the result

Don't trust `curl -k` or bare `openssl s_client` as proof the cert covers the
right hostname — both skip hostname verification, which is exactly what you're
trying to confirm. Use plain `curl` (no `-k`) or
`openssl s_client -connect host:443 -servername host -verify_hostname host`,
and where possible get confirmation from a device that never touched the
issuing environment (a different machine/network) — see
`references/verify-hostname-match.md`.

## Self-improvement

If you hit a new hosting-panel quirk (different challenge path, SFTP instead
of FTP, a docroot that isn't domain-uniform per alias), extend
`scripts/get_cert.py` and note the quirk in `references/`. This skill is
meant to survive across projects — the whole point is not re-deriving this
from scratch next time a redirect domain needs a certificate.
