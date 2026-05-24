from __future__ import annotations

import uuid
from pathlib import Path


def load_or_create_installation_id(path: Path) -> str:
    if path.exists():
        existing = path.read_text(encoding="utf-8").strip()
        if existing:
            return existing

    installation_id = uuid.uuid4().hex
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(f"{installation_id}\n", encoding="utf-8")
    try:
        path.chmod(0o600)
    except OSError:
        pass
    return installation_id
