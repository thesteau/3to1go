from __future__ import annotations

import os
import secrets
from pathlib import Path


def load_auth_token() -> str:
    raw_path = os.getenv("AUTH_TOKEN_FILE")
    if raw_path and raw_path.strip():
        return _load_or_create_auth_token(Path(raw_path.strip()))

    raise RuntimeError("AUTH_TOKEN_FILE environment variable is not set or empty")


def _load_or_create_auth_token(path: Path) -> str:
    path.parent.mkdir(parents=True, exist_ok=True)

    existing = _read_auth_token(path)
    if existing is not None:
        return existing

    token = secrets.token_urlsafe(32)
    try:
        with path.open("x", encoding="utf-8") as handle:
            handle.write(f"{token}\n")
        _set_owner_only_permissions(path)
        return token
    except FileExistsError:
        existing = _read_auth_token(path)
        if existing is not None:
            return existing

    path.write_text(f"{token}\n", encoding="utf-8")
    _set_owner_only_permissions(path)
    return token


def _read_auth_token(path: Path) -> str | None:
    if not path.exists():
        return None
    token = path.read_text(encoding="utf-8").strip()
    return token or None


def _set_owner_only_permissions(path: Path) -> None:
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass
