#!/usr/bin/env python3
"""Minimal ACME v2 client (HTTP-01), stdlib + openssl subprocess only.
No certbot, no acme/cryptography libraries, no third-party account signup.
See ../SKILL.md for when/why to use this instead of a real ACME client."""
import sys
sys.stdout.reconfigure(encoding="utf-8")
sys.stderr.reconfigure(encoding="utf-8")
import os, json, base64, hashlib, subprocess, time, argparse, shutil
import urllib.request, urllib.error, ftplib, io

CA_DIR = "https://acme-v02.api.letsencrypt.org/directory"


def find_openssl():
    """openssl is usually NOT on PATH for a plain Windows PowerShell session
    even when it's on PATH for a Git-Bash-backed tool -- fall back to the
    Git for Windows bundled copy rather than crash with FileNotFoundError."""
    found = shutil.which("openssl")
    if found:
        return found
    for candidate in (r"C:\Program Files\Git\mingw64\bin\openssl.exe",
                       r"C:\Program Files\Git\usr\bin\openssl.exe"):
        if os.path.exists(candidate):
            return candidate
    return "openssl"  # let it fail loudly if truly nowhere


OPENSSL = find_openssl()


def parse_args():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--domain", action="append", required=True, dest="domains",
                    help="Domain to include (repeatable). First domain names the output files.")
    p.add_argument("--ftp-host", required=True)
    p.add_argument("--ftp-user", required=True)
    p.add_argument("--docroot", required=True,
                    help="FTP-relative path to the shared document root, e.g. /example.com/docs")
    p.add_argument("--out-dir", default=os.path.join(os.path.expanduser("~"), "Desktop"))
    p.add_argument("--work-dir", default=os.path.dirname(os.path.abspath(__file__)))
    return p.parse_args()


def ftp_conn(host, user, pw):
    f = ftplib.FTP(host)
    f.login(user, pw)
    return f


def ftp_upload_challenge(host, user, pw, docroot, token, content):
    f = ftp_conn(host, user, pw)
    f.cwd(docroot)
    for d in (".well-known", "acme-challenge"):
        try:
            f.mkd(d)
        except ftplib.error_perm:
            pass
        f.cwd(d)
    f.storbinary(f"STOR {token}", io.BytesIO(content.encode("ascii")))
    f.quit()


def run(cmd, input_bytes=None):
    p = subprocess.run(cmd, input=input_bytes, capture_output=True)
    if p.returncode != 0:
        raise RuntimeError("cmd failed: %s\n%s" % (cmd, p.stderr.decode(errors="replace")))
    return p.stdout


def b64(b):
    return base64.urlsafe_b64encode(b).decode().rstrip("=")


def openssl_sign(keyfile, data):
    return run([OPENSSL, "dgst", "-sha256", "-sign", keyfile], data)


def get_jwk(keyfile):
    out = run([OPENSSL, "rsa", "-in", keyfile, "-noout", "-modulus"]).decode()
    modulus_hex = out.strip().split("=")[1]
    n = b64(bytes.fromhex(modulus_hex))
    return {"kty": "RSA", "n": n, "e": b64((65537).to_bytes(3, "big").lstrip(b"\x00"))}


def jwk_thumbprint(jwk):
    canon = json.dumps({"e": jwk["e"], "kty": jwk["kty"], "n": jwk["n"]}, separators=(",", ":"))
    return b64(hashlib.sha256(canon.encode()).digest())


def http(url, data=None, headers=None, method=None):
    req = urllib.request.Request(url, data=data, headers=headers or {}, method=method)
    try:
        resp = urllib.request.urlopen(req)
        return resp.getcode(), resp.read(), dict(resp.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read(), dict(e.headers)


def main():
    args = parse_args()
    os.makedirs(args.work_dir, exist_ok=True)
    os.makedirs(args.out_dir, exist_ok=True)

    account_key = os.path.join(args.work_dir, "acme_account.key")
    domain_key = os.path.join(args.work_dir, "domain.key")
    csr_path = os.path.join(args.work_dir, "domain.csr")
    label = args.domains[0].replace("*", "wildcard")

    # Never accept the password as a CLI arg or hardcode it — runtime prompt only.
    ftp_pw = input(f"FTP password for {args.ftp_user} (visible): ")

    print("=== 1. Keys ===")
    if not os.path.exists(account_key):
        run([OPENSSL, "genrsa", "-out", account_key, "4096"])
    run([OPENSSL, "genrsa", "-out", domain_key, "4096"])
    san = ",".join("DNS:%s" % d for d in args.domains)
    run([OPENSSL, "req", "-new", "-sha256", "-key", domain_key, "-subj", "/",
         "-addext", "subjectAltName=%s" % san, "-out", csr_path])
    print("keys and CSR ready")

    jwk = get_jwk(account_key)
    thumb = jwk_thumbprint(jwk)

    print("=== 2. ACME directory ===")
    _, body, _ = http(CA_DIR)
    directory = json.loads(body)
    _, _, hdrs = http(directory["newNonce"], method="HEAD")
    nonce = hdrs["Replay-Nonce"]
    kid = [None]

    def signed_request(url, payload):
        nonlocal nonce
        protected = {"alg": "RS256", "nonce": nonce, "url": url}
        if kid[0]:
            protected["kid"] = kid[0]
        else:
            protected["jwk"] = jwk
        protected_b64 = b64(json.dumps(protected).encode())
        payload_b64 = "" if payload is None else b64(json.dumps(payload).encode())
        signing_input = ("%s.%s" % (protected_b64, payload_b64)).encode()
        sig = openssl_sign(account_key, signing_input)
        req_body = json.dumps({"protected": protected_b64, "payload": payload_b64, "signature": b64(sig)}).encode()
        code, resp_body, resp_hdrs = http(url, data=req_body, headers={"Content-Type": "application/jose+json"}, method="POST")
        nonce = resp_hdrs.get("Replay-Nonce", nonce)
        return code, resp_body, resp_hdrs

    print("=== 3. Account registration ===")
    code, body, hdrs = signed_request(directory["newAccount"], {"termsOfServiceAgreed": True})
    if code not in (200, 201):
        print("Account ERROR:", code, body.decode()); sys.exit(1)
    kid[0] = hdrs["Location"]
    print("account:", kid[0])

    print("=== 4. New order ===")
    order_payload = {"identifiers": [{"type": "dns", "value": d} for d in args.domains]}
    code, body, hdrs = signed_request(directory["newOrder"], order_payload)
    if code != 201:
        print("Order ERROR:", code, body.decode()); sys.exit(1)
    order = json.loads(body)
    order_url = hdrs["Location"]
    print("order:", order_url)

    print("=== 5. HTTP-01 for each domain ===")
    for authz_url in order["authorizations"]:
        code, body, _ = signed_request(authz_url, None)
        authz = json.loads(body)
        domain = authz["identifier"]["value"]
        if authz["status"] == "valid":
            print("%s: already valid, skipping" % domain)
            continue
        chal = next(c for c in authz["challenges"] if c["type"] == "http-01")
        token = chal["token"]
        keyauth = "%s.%s" % (token, thumb)

        print("%s: token %s -- uploading via FTP" % (domain, token))
        ftp_upload_challenge(args.ftp_host, args.ftp_user, ftp_pw, args.docroot, token, keyauth)

        check_url = "http://%s/.well-known/acme-challenge/%s" % (domain, token)
        for attempt in range(5):
            code2, body2, _ = http(check_url)
            if code2 == 200 and body2.decode().strip() == keyauth:
                print("%s: file serves correctly, verified locally" % domain)
                break
            time.sleep(2)
        else:
            print("%s: WARNING -- local challenge check failed, trying ACME anyway" % domain)

        print("%s: telling ACME to validate" % domain)
        code, body, _ = signed_request(chal["url"], {})
        if code not in (200, 201):
            print("%s: validation trigger ERROR:" % domain, code, body.decode()); sys.exit(1)

        for attempt in range(20):
            code, body, _ = signed_request(authz_url, None)
            authz = json.loads(body)
            if authz["status"] == "valid":
                print("%s: VALID" % domain)
                break
            if authz["status"] == "invalid":
                print("%s: INVALID --" % domain, json.dumps(authz)); sys.exit(1)
            time.sleep(3)
        else:
            print("%s: did not confirm in time" % domain); sys.exit(1)

    print("=== 6. Finalize ===")
    csr_der = run([OPENSSL, "req", "-in", csr_path, "-outform", "DER"])
    code, body, hdrs = signed_request(order["finalize"], {"csr": b64(csr_der)})
    if code != 200:
        print("Finalize ERROR:", code, body.decode()); sys.exit(1)

    for attempt in range(20):
        code, body, _ = signed_request(order_url, None)
        order = json.loads(body)
        if order["status"] == "valid":
            break
        if order["status"] == "invalid":
            print("ORDER INVALID:", json.dumps(order)); sys.exit(1)
        time.sleep(3)
    else:
        print("certificate not ready in time"); sys.exit(1)

    print("=== 7. Downloading certificate ===")
    code, cert_body, _ = signed_request(order["certificate"], None)

    out_cert = os.path.join(args.out_dir, f"{label}_certificate.pem")
    out_key = os.path.join(args.out_dir, f"{label}_private.key")
    with open(out_cert, "wb") as f:
        f.write(cert_body)
    with open(domain_key, "rb") as src, open(out_key, "wb") as dst:
        dst.write(src.read())

    print("DONE:")
    print("  certificate (fullchain):", out_cert)
    print("  private key:", out_key)
    print("  -> upload both through the hosting panel's certificate-install form")


if __name__ == "__main__":
    main()
