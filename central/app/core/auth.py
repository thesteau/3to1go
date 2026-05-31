from __future__ import annotations

import os
import secrets
from pathlib import Path


_DEFAULT_SECRET_DIR = Path("/run/secrets")


def load_auth_token() -> str:
    raw_path = os.getenv("AUTH_TOKEN_FILE")
    if raw_path and raw_path.strip():
        return _load_or_create_auth_token(_resolve_auth_token_path(raw_path.strip()))

    raise RuntimeError("AUTH_TOKEN_FILE environment variable is not set or empty.")


def _resolve_auth_token_path(value: str) -> Path:
    candidate = Path(value)
    if candidate.is_absolute():
        return candidate
    if len(candidate.parts) == 1:
        return _DEFAULT_SECRET_DIR / candidate
    return candidate


def _load_or_create_auth_token(path: Path) -> str:
    _validate_auth_token_path(path)
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
    _validate_auth_token_path(path)
    token = path.read_text(encoding="utf-8").strip()
    return token or None


def _set_owner_only_permissions(path: Path) -> None:
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass


def _validate_auth_token_path(path: Path) -> None:
    if path.is_dir():
        raise RuntimeError(
            "AUTH_TOKEN_FILE must point to a file, but "
            f"'{path}' is a directory. If you are using Docker bind mounts, "
            "the host path likely did not exist and Docker created a directory instead. "
            "Mount './secrets:/run/secrets' for auto-generation, or create the token file on the host first."
        )
