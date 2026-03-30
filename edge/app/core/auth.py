from __future__ import annotations

import os
from pathlib import Path



def load_auth_token() -> str:
    env_token = os.getenv("AUTH_TOKEN")
    if env_token and env_token.strip():
        return env_token.strip()

    raw_path = os.getenv("AUTH_TOKEN_FILE")
    if raw_path and raw_path.strip():
        return _load_auth_token_from_file(Path(raw_path.strip()))

    return "change-me"



def _load_auth_token_from_file(path: Path) -> str:
    if not path.exists():
        raise RuntimeError(
            f"AUTH_TOKEN_FILE does not exist: {path}. Start Central first so it can create the shared token, or set AUTH_TOKEN explicitly."
        )

    token = path.read_text(encoding="utf-8").strip()
    if not token:
        raise RuntimeError(
            f"AUTH_TOKEN_FILE is empty: {path}. Regenerate the token file from Central or set AUTH_TOKEN explicitly."
        )
    return token
