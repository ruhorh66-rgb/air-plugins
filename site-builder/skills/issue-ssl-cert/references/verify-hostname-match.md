# Verifying a certificate actually covers the hostname

`curl -k` and bare `openssl s_client` (no `-verify_hostname`) both **skip**
hostname verification — the exact check you're trying to confirm passed. A
"looks fine" result from either proves nothing about whether the cert's SAN
list includes the domain you care about.

Use one of:

```bash
curl https://the-domain/                      # no -k; a real failure is real
openssl s_client -connect the-domain:443 -servername the-domain -verify_hostname the-domain
```

Where possible, also get a check from a device that never shared the issuing
machine's network path (a phone off VPN, a different machine) — a local VPN
kill-switch or proxy can itself produce a misleading TLS error that looks
identical to a real certificate/hostname mismatch, in either direction. Trust
the check that isolates the variable you're actually testing (the cert), not
the one that's easiest to run first.
