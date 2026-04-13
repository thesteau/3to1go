from __future__ import annotations

import os
from pathlib import Path


def load_auth_token(path: Path | None = None) -> str:
    if path is not None:
        return _load_auth_token_from_file(path)

    raw_path = os.getenv("AUTH_TOKEN_FILE")
    if raw_path and raw_path.strip():
        return _load_auth_token_from_file(Path(raw_path.strip()).expanduser())

    raise RuntimeError("AUTH_TOKEN_FILE environment variable is not set or empty.")


def _load_auth_token_from_file(path: Path) -> str:
    if not path.exists():
        raise RuntimeError(
            f"AUTH_TOKEN_FILE does not exist: {path}. Provide the token file on the Edge device before startup."
        )

    token = path.read_text(encoding="utf-8").strip()
    if not token:
        raise RuntimeError(
            f"AUTH_TOKEN_FILE is empty: {path}. Populate the token file on the Edge device before startup."
        )
    return token
