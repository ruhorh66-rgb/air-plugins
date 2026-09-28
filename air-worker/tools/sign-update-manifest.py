#!/usr/bin/env python3
"""Sign an AirWorker update manifest with an external Ed25519 private key."""

from __future__ import annotations

import argparse
import base64
import json
from pathlib import Path

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey


def canonical_message(m: dict) -> bytes:
    cli = m["cli"]
    tray = m["tray"]
    parts = [
        str(int(m["schema"])),
        str(m["channel"]).strip().lower(),
        str(m["version"]).strip(),
        str(m["published_at"]).strip(),
        str(m["vcs_revision"]).strip().lower(),
        str(m["payload_sha256"]).strip().lower(),
        str(cli["name"]),
        str(cli["url"]),
        str(cli["sha256"]).strip().lower(),
        str(int(cli["size"])),
        str(tray["name"]),
        str(tray["url"]),
        str(tray["sha256"]).strip().lower(),
        str(int(tray["size"])),
        str(m["signing_key_id"]).strip(),
        str(m.get("release_notes_url", "")).strip(),
    ]
    return "\n".join(parts).encode("utf-8")


def read_b64(path: Path) -> bytes:
    return base64.b64decode(path.read_text(encoding="ascii").strip(), validate=True)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--manifest", required=True, type=Path)
    ap.add_argument("--private-key", required=True, type=Path)
    ap.add_argument("--public-key", required=True, type=Path)
    ap.add_argument("--key-id", required=True)
    args = ap.parse_args()

    manifest = json.loads(args.manifest.read_text(encoding="utf-8-sig"))
    if int(manifest.get("schema", 0)) != 1:
        raise SystemExit("unsupported manifest schema")
    if str(manifest.get("signing_key_id", "")).strip() != args.key_id:
        raise SystemExit("signing key id mismatch")

    private_raw = read_b64(args.private_key)
    public_expected = read_b64(args.public_key)
    if len(private_raw) != 32 or len(public_expected) != 32:
        raise SystemExit("Ed25519 key length is invalid")

    private = Ed25519PrivateKey.from_private_bytes(private_raw)
    public_actual = private.public_key().public_bytes(
        serialization.Encoding.Raw,
        serialization.PublicFormat.Raw,
    )
    if public_actual != public_expected:
        raise SystemExit("private key does not match committed public key")

    manifest["manifest_signature"] = base64.b64encode(
        private.sign(canonical_message(manifest))
    ).decode("ascii")

    args.manifest.write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
        newline="\n",
    )
    print(
        json.dumps(
            {
                "signed": True,
                "version": manifest["version"],
                "channel": manifest["channel"],
                "signing_key_id": manifest["signing_key_id"],
            },
            ensure_ascii=False,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
