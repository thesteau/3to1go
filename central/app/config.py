from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from dotenv import load_dotenv


def _env_bool(name: str, default: bool) -> bool:
    raw_value = os.getenv(name)
    if raw_value is None:
        return default
    return raw_value.strip().lower() in {"1", "true", "yes", "on"}


@dataclass(slots=True)
class Settings:
    auth_token: str
    storage_backend: str
    backup_root: Path
    retention_keep_last: int
    log_level: str
    max_upload_size_mb: int
    staging_dir: Path

    @property
    def max_upload_size_bytes(self) -> int:
        return self.max_upload_size_mb * 1024 * 1024


def load_settings() -> Settings:
    load_dotenv()

    return Settings(
        auth_token=os.getenv("AUTH_TOKEN", "change-me"),
        storage_backend=os.getenv("STORAGE_BACKEND", "local").strip().lower(),
        backup_root=Path(os.getenv("BACKUP_ROOT", "/backups")),
        retention_keep_last=max(1, int(os.getenv("RETENTION_KEEP_LAST", "3"))),
        log_level=os.getenv("LOG_LEVEL", "INFO").upper(),
        max_upload_size_mb=max(1, int(os.getenv("MAX_UPLOAD_SIZE_MB", "2048"))),
        staging_dir=Path(os.getenv("STAGING_DIR", "/staging")),
    )

