from __future__ import annotations

import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import quote

from app.core.auth import load_auth_token


APP_DIR_NAME = "RelayCentralizerCentral"


def _default_config_dir() -> Path:
    home = Path.home()
    if sys.platform == "darwin":
        return home / "Library" / "Application Support" / APP_DIR_NAME

    xdg_config_home = os.getenv("XDG_CONFIG_HOME")
    if xdg_config_home and xdg_config_home.strip():
        return Path(xdg_config_home.strip()) / APP_DIR_NAME
    return home / ".config" / APP_DIR_NAME


def settings_storage_path() -> Path:
    return _default_config_dir() / "settings.json"


def _coerce_int(value: Any, default: int, minimum: int) -> int:
    if value is None or value == "":
        return default
    return max(minimum, int(value))


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
    upload_cleanup_interval_seconds: int
    staging_dir: Path
    http_host: str
    http_port: int
    index_database_url: str | None = None

    @property
    def max_upload_size_bytes(self) -> int:
        return self.max_upload_size_mb * 1024 * 1024

    @property
    def upload_chunk_size_bytes(self) -> int:
        return self.upload_chunk_size_mb * 1024 * 1024


def settings_to_payload(settings: Settings) -> dict[str, Any]:
    return {
        "retention_keep_last": settings.retention_keep_last,
        "log_level": settings.log_level,
        "max_upload_size_mb": settings.max_upload_size_mb,
        "upload_chunk_size_mb": settings.upload_chunk_size_mb,
        "upload_session_ttl_hours": settings.upload_session_ttl_hours,
        "upload_cleanup_interval_seconds": settings.upload_cleanup_interval_seconds,
    }


def build_settings(payload: dict[str, Any] | None = None) -> Settings:
    raw = payload or {}
    return Settings(
        auth_token=load_auth_token(),
        storage_backend=os.getenv("STORAGE_BACKEND", "local").strip().lower(),
        index_database_url=_build_index_database_url(),
        backup_root=Path(os.getenv("BACKUP_ROOT", "/backups")),
        retention_keep_last=_coerce_int(raw.get("retention_keep_last"), 3, 1),
        log_level=str(raw.get("log_level") or "INFO").strip().upper() or "INFO",
        max_upload_size_mb=_coerce_int(raw.get("max_upload_size_mb"), 2048, 1),
        upload_chunk_size_mb=_coerce_int(raw.get("upload_chunk_size_mb"), 8, 1),
        upload_session_ttl_hours=_coerce_int(raw.get("upload_session_ttl_hours"), 24, 1),
        upload_cleanup_interval_seconds=_coerce_int(raw.get("upload_cleanup_interval_seconds"), 300, 10),
        staging_dir=Path(os.getenv("STAGING_DIR", "/staging")),
        http_host=os.getenv("HTTP_HOST", "0.0.0.0"),
        http_port=max(1, int(os.getenv("HTTP_PORT", "8000"))),
    )


def _build_index_database_url() -> str | None:
    explicit_url = os.getenv("INDEX_DATABASE_URL", "").strip()
    if explicit_url:
        return explicit_url

    username = os.getenv("INDEX_DATABASE_USER", "").strip() or os.getenv("POSTGRES_USER", "").strip()
    password = os.getenv("INDEX_DATABASE_PASSWORD", "").strip() or os.getenv("POSTGRES_PASSWORD", "").strip()
    if not username or not password:
        return None

    host = os.getenv("INDEX_DATABASE_HOST", "postgres").strip() or "postgres"
    port = os.getenv("INDEX_DATABASE_PORT", "5432").strip() or "5432"
    database = (
        os.getenv("INDEX_DATABASE_NAME", "").strip()
        or os.getenv("POSTGRES_DB", "").strip()
        or "relaycentral"
    )
    return f"postgresql://{quote(username)}:{quote(password)}@{host}:{port}/{database}"


def load_settings() -> Settings:
    path = settings_storage_path()
    payload: dict[str, Any] = {}
    if path.exists():
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
            if isinstance(data, dict):
                payload = data
        except (json.JSONDecodeError, OSError):
            pass
    return build_settings(payload)
