"""Create a portable encrypted backup of the scoped Windows AirWorker signer."""
from __future__ import annotations

import argparse
import base64
import getpass
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import warnings

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.scrypt import Scrypt

KEY_ID = "air-worker-update-2026-02"
FINGERPRINT = "08f6b7e4f44bb0398bdb1628cb518870b890abd055e2c7fb3d403085395fe790"
AAD = ("AirWorker escrow v1\n" + KEY_ID + "\n" + FINGERPRINT).encode("ascii")


def fingerprint(seed: bytes) -> str:
    public = Ed25519PrivateKey.from_private_bytes(seed).public_key()
    return hashlib.sha256(public.public_bytes(
        serialization.Encoding.Raw, serialization.PublicFormat.Raw,
    )).hexdigest()


def derive(password: str, salt: bytes) -> bytes:
    return Scrypt(salt=salt, length=32, n=2**17, r=8, p=1).derive(password.encode("utf-8"))


def encode(seed: bytes, password: str) -> dict:
    if len(seed) != 32 or fingerprint(seed) != FINGERPRINT:
        raise SystemExit("Credential does not match the pinned AirWorker signer")
    salt, nonce = os.urandom(16), os.urandom(12)
    ct = AESGCM(derive(password, salt)).encrypt(nonce, seed, AAD)
    return {
        "schema": "air-worker.signer-escrow/v1", "key_id": KEY_ID,
        "public_fingerprint_sha256": FINGERPRINT,
        "salt": base64.b64encode(salt).decode("ascii"),
        "nonce": base64.b64encode(nonce).decode("ascii"),
        "ciphertext": base64.b64encode(ct).decode("ascii"),
    }


def verify(envelope: dict, password: str) -> None:
    if (envelope.get("schema"), envelope.get("key_id"), envelope.get("public_fingerprint_sha256")) != (
        "air-worker.signer-escrow/v1", KEY_ID, FINGERPRINT,
    ):
        raise SystemExit("Unexpected escrow identity")
    salt, nonce, ct = (base64.b64decode(envelope[k], validate=True) for k in ("salt", "nonce", "ciphertext"))
    if (len(salt), len(nonce), len(ct)) != (16, 12, 48):
        raise SystemExit("Malformed escrow envelope")
    from cryptography.exceptions import InvalidTag
    try:
        seed = AESGCM(derive(password, salt)).decrypt(nonce, ct, AAD)
    except InvalidTag:
        raise SystemExit("Escrow password does not match or ciphertext is damaged") from None
    if fingerprint(seed) != FINGERPRINT:
        raise SystemExit("Restored key does not match its public fingerprint")
    key = Ed25519PrivateKey.from_private_bytes(seed)
    challenge = b"AirWorker escrow verification"
    key.public_key().verify(key.sign(challenge), challenge)


def read_password() -> str:
    if not sys.stdin.isatty() or not sys.stderr.isatty():
        raise SystemExit("Run in a local interactive terminal; hidden input is mandatory")
    with warnings.catch_warnings():
        warnings.simplefilter("error", getpass.GetPassWarning)
        password = getpass.getpass("AirWorker recovery password (saved separately, hidden): ")
    if len(password) < 32:
        raise SystemExit("Use a password-manager-generated password of at least 32 random characters")
    return password


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("backup", "verify"))
    parser.add_argument("file", type=Path)
    args = parser.parse_args()
    password = read_password()
    if args.mode == "backup":
        if os.name != "nt":
            raise SystemExit("Backup runs on the Windows release operator's host")
        if args.file.exists():
            raise SystemExit("Backup already exists; refusing to overwrite")
        with warnings.catch_warnings():
            warnings.simplefilter("error", getpass.GetPassWarning)
            confirmation = getpass.getpass("Repeat password (hidden): ")
        if password != confirmation:
            raise SystemExit("Passwords do not match")
        spec = importlib.util.spec_from_file_location("airworker_update_signer",
                                                      Path(__file__).with_name("sign-update-manifest.py"))
        if spec is None or spec.loader is None:
            raise SystemExit("Cannot load the release signer")
        signer = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(signer)
        envelope = encode(signer.read_prerelease_windows_key(), password)
        verify(envelope, password)
        with args.file.open("x", encoding="utf-8", newline="\n") as f:
            f.write(json.dumps(envelope, indent=2) + "\n")
    if args.file.stat().st_size > 4096:
        raise SystemExit("Oversized escrow file")
    verify(json.loads(args.file.read_text(encoding="utf-8")), password)
    print("PASS: recovered signing key matches the pinned public fingerprint.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
