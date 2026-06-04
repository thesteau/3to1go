from __future__ import annotations

import base64
import json
import os
import time
import uuid
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey


_DEFAULT_TTL_DAYS = 365


def _b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def _b64url_decode(s: str) -> bytes:
    padding = 4 - len(s) % 4
    if padding != 4:
        s += "=" * padding
    return base64.urlsafe_b64decode(s)


def load_or_create_issuer_keypair(path: Path) -> tuple[Ed25519PrivateKey, Ed25519PublicKey]:
    if path.is_dir():
        raise RuntimeError(
            "ISSUER_KEY_FILE must point to a file, but "
            f"'{path}' is a directory. If you are using Docker bind mounts, "
            "the host path likely did not exist and Docker created a directory instead. "
            "Mount './secrets:/run/secrets' for auto-generation, or create the key file on the host first."
        )
    if path.exists():
        raw = path.read_bytes()
        if len(raw) == 32:
            private_key = Ed25519PrivateKey.from_private_bytes(raw)
            return private_key, private_key.public_key()
        raise RuntimeError(
            f"Issuer key file '{path}' has unexpected length {len(raw)}. "
            "Expected 32 bytes (Ed25519 seed). The file may be corrupt."
        )

    private_key = Ed25519PrivateKey.generate()
    raw = private_key.private_bytes_raw()
    path.parent.mkdir(parents=True, exist_ok=True)
    try:
        with path.open("xb") as handle:
            handle.write(raw)
    except FileExistsError:
        return load_or_create_issuer_keypair(path)
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass
    return private_key, private_key.public_key()


def public_key_to_bytes(public_key: Ed25519PublicKey) -> bytes:
    return public_key.public_bytes_raw()


def public_key_from_bytes(raw: bytes) -> Ed25519PublicKey:
    return Ed25519PublicKey.from_public_bytes(raw)


def mint_credential(private_key: Ed25519PrivateKey, ttl_days: int = _DEFAULT_TTL_DAYS) -> str:
    now = int(time.time())
    header = _b64url(json.dumps({"alg": "EdDSA", "typ": "RCT"}, separators=(",", ":")).encode())
    payload = _b64url(
        json.dumps(
            {"iat": now, "exp": now + ttl_days * 86400, "jti": str(uuid.uuid4())},
            separators=(",", ":"),
        ).encode()
    )
    signing_input = f"{header}.{payload}".encode()
    sig = _b64url(private_key.sign(signing_input))
    return f"{header}.{payload}.{sig}"


def verify_credential(token: str, public_key: Ed25519PublicKey, revoked: frozenset[str]) -> None:
    """Raises ValueError on any verification failure."""
    parts = token.split(".")
    if len(parts) != 3:
        raise ValueError("malformed token")

    header_b64, payload_b64, sig_b64 = parts

    try:
        sig = _b64url_decode(sig_b64)
        public_key.verify(sig, f"{header_b64}.{payload_b64}".encode())
    except Exception as exc:
        raise ValueError("invalid signature") from exc

    try:
        payload = json.loads(_b64url_decode(payload_b64))
    except Exception as exc:
        raise ValueError("malformed payload") from exc

    if payload.get("exp", 0) < int(time.time()):
        raise ValueError("credential expired")

    jti = payload.get("jti")
    if not jti:
        raise ValueError("credential missing jti")
    if jti in revoked:
        raise ValueError("credential revoked")


def load_revoked_credentials(path: Path | None) -> frozenset[str]:
    if path is None or not path.exists():
        return frozenset()
    lines = path.read_text(encoding="utf-8").splitlines()
    return frozenset(line.strip() for line in lines if line.strip())
