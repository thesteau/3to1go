from __future__ import annotations

import os
from pathlib import Path

from app.core.signing import load_or_create_issuer_keypair, load_revoked_credentials


_DEFAULT_SECRET_DIR = Path("/run/secrets")


def issuer_key_path_from_env() -> Path:
    raw = os.getenv("ISSUER_KEY_FILE")
    if raw and raw.strip():
        return _resolve_path(raw.strip())
    raise RuntimeError("ISSUER_KEY_FILE environment variable is not set or empty.")


def revoked_credentials_path_from_env() -> Path | None:
    raw = os.getenv("REVOKED_CREDENTIALS_FILE")
    if raw and raw.strip():
        return Path(raw.strip())
    return None


def _resolve_path(value: str) -> Path:
    candidate = Path(value)
    if candidate.is_absolute():
        return candidate
    if len(candidate.parts) == 1:
        return _DEFAULT_SECRET_DIR / candidate
    return candidate


__all__ = [
    "issuer_key_path_from_env",
    "revoked_credentials_path_from_env",
    "load_or_create_issuer_keypair",
    "load_revoked_credentials",
]
