from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from app.core.auth import load_auth_token


@dataclass(slots=True)
class Settings:
    auth_token: str
    storage_backend: str
    backup_root: Path
    retention_keep_last: int
    log_level: str
    max_upload_size_mb: int
    upload_chunk_size_mb: int
    upload_session_ttl_hours: int
    staging_dir: Path
    http_host: str
    http_port: int

    @property
    def max_upload_size_bytes(self) -> int:
        return self.max_upload_size_mb * 1024 * 1024

    @property
    def upload_chunk_size_bytes(self) -> int:
        return self.upload_chunk_size_mb * 1024 * 1024


def load_settings() -> Settings:
    return Settings(
        auth_token=load_auth_token(),
        storage_backend=os.getenv("STORAGE_BACKEND", "local").strip().lower(),
        backup_root=Path(os.getenv("BACKUP_ROOT", "/backups")),
        retention_keep_last=max(1, int(os.getenv("RETENTION_KEEP_LAST", "3"))),
        log_level=os.getenv("LOG_LEVEL", "INFO").upper(),
        max_upload_size_mb=max(1, int(os.getenv("MAX_UPLOAD_SIZE_MB", "2048"))),
        upload_chunk_size_mb=max(1, int(os.getenv("UPLOAD_CHUNK_SIZE_MB", "8"))),
        upload_session_ttl_hours=max(1, int(os.getenv("UPLOAD_SESSION_TTL_HOURS", "24"))),
        staging_dir=Path(os.getenv("STAGING_DIR", "/staging")),
        http_host=os.getenv("HTTP_HOST", "0.0.0.0"),
        http_port=max(1, int(os.getenv("HTTP_PORT", "8000"))),
    )
