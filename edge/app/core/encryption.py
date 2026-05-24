from __future__ import annotations

import base64
import os
from hashlib import sha256
from pathlib import Path

MAGIC = b"RCENC1\x00\x00"
_IV_SIZE = 12


def generate_key() -> bytes:
    return os.urandom(32)


def load_or_create_key(path: Path) -> bytes:
    if path.exists():
        data = path.read_bytes()
        if len(data) == 32:
            return data
    key = generate_key()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(key)
    try:
        path.chmod(0o600)
    except OSError:
        pass
    return key


def key_as_base64(key: bytes) -> str:
    return base64.urlsafe_b64encode(key).decode()


def key_fingerprint(key: bytes) -> str:
    return sha256(key).hexdigest()


def encrypt_file(key: bytes, src: Path, dst: Path) -> None:
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    iv = os.urandom(_IV_SIZE)
    ciphertext = AESGCM(key).encrypt(iv, src.read_bytes(), None)
    dst.write_bytes(MAGIC + iv + ciphertext)
