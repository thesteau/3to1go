from __future__ import annotations

import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import quote, urlparse

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


def hook_scripts_dir() -> Path:
    return _default_config_dir() / "hook-scripts"


def _coerce_int(value: Any, default: int, minimum: int) -> int:
    if value is None or value == "":
        return default
    return max(minimum, int(value))


def _coerce_text(value: Any, default: str = "") -> str:
    if value is None:
        return default
    normalized = str(value).strip()
    return normalized or default


def _coerce_url(value: Any, default: str = "") -> str:
    normalized = _coerce_text(value, default).rstrip("/")
    if not normalized:
        return ""
    parsed = urlparse(normalized)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise ValueError("url must be a full http or https URL")
    return normalized



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
    ntfy_url: str
    ntfy_topic: str
    ntfy_message_template: str
    ntfy_match_edge_id: str
    ntfy_match_edge_instance_id: str
    ntfy_match_source: str
    hook_pre_command: str
    hook_post_command: str
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
        "ntfy_url": settings.ntfy_url,
        "ntfy_topic": settings.ntfy_topic,
        "ntfy_message_template": settings.ntfy_message_template,
        "ntfy_match_edge_id": settings.ntfy_match_edge_id,
        "ntfy_match_edge_instance_id": settings.ntfy_match_edge_instance_id,
        "ntfy_match_source": settings.ntfy_match_source,
        "hook_pre_command": settings.hook_pre_command,
        "hook_post_command": settings.hook_post_command,
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
        ntfy_url=_coerce_url(raw.get("ntfy_url")),
        ntfy_topic=_coerce_text(raw.get("ntfy_topic")),
        ntfy_message_template=_coerce_text(raw.get("ntfy_message_template")),
        ntfy_match_edge_id=_coerce_text(raw.get("ntfy_match_edge_id")),
        ntfy_match_edge_instance_id=_coerce_text(raw.get("ntfy_match_edge_instance_id")),
        ntfy_match_source=_coerce_text(raw.get("ntfy_match_source")),
        hook_pre_command=_coerce_text(raw.get("hook_pre_command")),
        hook_post_command=_coerce_text(raw.get("hook_post_command")),
        staging_dir=Path(os.getenv("STAGING_DIR", "/staging")),
        http_host=os.getenv("HTTP_HOST", "0.0.0.0"),
        http_port=max(1, int(os.getenv("HTTP_PORT", "6555"))),
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
