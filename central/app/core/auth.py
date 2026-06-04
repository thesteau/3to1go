from __future__ import annotations

import os
from pathlib import Path


_DEFAULT_SECRET_DIR = Path("/run/secrets")
_DEFAULT_ISSUER_KEY_FILE = _DEFAULT_SECRET_DIR / "relay_issuer.key"


def issuer_key_path_from_env() -> Path:
    raw = os.getenv("ISSUER_KEY_FILE")
    if raw and raw.strip():
        return _resolve_path(raw.strip())
    return _DEFAULT_ISSUER_KEY_FILE


def _resolve_path(value: str) -> Path:
    candidate = Path(value)
    if candidate.is_absolute():
        return candidate
    if len(candidate.parts) == 1:
        return _DEFAULT_SECRET_DIR / candidate
    return candidate


__all__ = [
    "issuer_key_path_from_env",
]
