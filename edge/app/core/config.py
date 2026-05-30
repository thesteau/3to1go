from __future__ import annotations

import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from app.core.schedule import CronSchedule


APP_DIR_NAME = "RelayCentralizerEdge"
DEFAULT_SECRET_DIR = Path("/run/secrets")


def _default_scan_root() -> Path:
    if sys.platform == "win32":
        return Path("C:/Users")
    if sys.platform == "darwin":
        return Path("/Users")
    home_root = Path("/home")
    return home_root if home_root.exists() else Path("/")


def _default_config_dir() -> Path:
    home = Path.home()
    if sys.platform == "darwin":
        return home / "Library" / "Application Support" / APP_DIR_NAME

    xdg_config_home = os.getenv("XDG_CONFIG_HOME")
    if xdg_config_home and xdg_config_home.strip():
        return Path(xdg_config_home.strip()) / APP_DIR_NAME
    return home / ".config" / APP_DIR_NAME


def _default_state_dir() -> Path:
    home = Path.home()
    if sys.platform == "darwin":
        return _default_config_dir() / "state"

    xdg_state_home = os.getenv("XDG_STATE_HOME")
    if xdg_state_home and xdg_state_home.strip():
        return Path(xdg_state_home.strip()) / APP_DIR_NAME
    return home / ".local" / "state" / APP_DIR_NAME


def _default_spool_dir() -> Path:
    home = Path.home()
    if sys.platform == "darwin":
        return home / "Library" / "Caches" / APP_DIR_NAME / "spool"

    xdg_cache_home = os.getenv("XDG_CACHE_HOME")
    if xdg_cache_home and xdg_cache_home.strip():
        return Path(xdg_cache_home.strip()) / APP_DIR_NAME / "spool"
    return home / ".cache" / APP_DIR_NAME / "spool"


def _settings_path() -> Path:
    return _default_config_dir() / "settings.json"


def _coerce_bool(value: Any, default: bool) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        normalized = value.strip().lower()
        if normalized in {"1", "true", "yes", "on"}:
            return True
        if normalized in {"0", "false", "no", "off"}:
            return False
    if value is None:
        return default
    return bool(value)


def _coerce_text(value: Any, default: str) -> str:
    if value is None:
        return default
    normalized = str(value).strip()
    return normalized or default


def _coerce_int(value: Any, default: int, minimum: int) -> int:
    if value is None or value == "":
        return default
    return max(minimum, int(value))


@dataclass(slots=True)
class Settings:
    edge_id: str
    scan_root: Path
    central_url: str
    auth_token: str
    cron_schedule: str
    state_dir: Path
    spool_dir: Path
    log_level: str
    max_depth: int
    keep_local_pending: bool
    upload_chunk_size_mb: int
    min_upload_chunk_size_mb: int
    max_upload_chunk_size_mb: int
    upload_retry_max_attempts: int
    upload_retry_base_delay_seconds: int
    upload_retry_max_delay_seconds: int
    upload_connect_timeout_seconds: int
    upload_read_timeout_padding_seconds: int
    upload_min_throughput_bytes_per_second: int
    circuit_breaker_failure_threshold: int
    circuit_breaker_cooldown_seconds: int
    http_host: str
    http_port: int

    @property
    def upload_chunk_size_bytes(self) -> int:
        return self.upload_chunk_size_mb * 1024 * 1024

    @property
    def min_upload_chunk_size_bytes(self) -> int:
        return self.min_upload_chunk_size_mb * 1024 * 1024

    @property
    def max_upload_chunk_size_bytes(self) -> int:
        return self.max_upload_chunk_size_mb * 1024 * 1024


def settings_storage_path() -> Path:
    return _settings_path()


def encryption_key_path() -> Path:
    return _default_config_dir() / "encryption.key"


def installation_id_path() -> Path:
    return _default_config_dir() / "installation.id"


def _resolve_auth_token_file_path(value: str) -> Path:
    candidate = Path(value.strip())
    if candidate.is_absolute():
        return candidate

    # In the bundled Docker layouts, bare filenames live under /run/secrets.
    if len(candidate.parts) == 1:
        return DEFAULT_SECRET_DIR / candidate

    return candidate


def settings_to_payload(settings: Settings) -> dict[str, Any]:
    return {
        "edge_id": settings.edge_id,
        "scan_root": str(settings.scan_root),
        "central_url": settings.central_url,
        "auth_token": settings.auth_token,
        "cron_schedule": settings.cron_schedule,
        "state_dir": str(settings.state_dir),
        "spool_dir": str(settings.spool_dir),
        "log_level": settings.log_level,
        "max_depth": settings.max_depth,
        "keep_local_pending": settings.keep_local_pending,
        "upload_chunk_size_mb": settings.upload_chunk_size_mb,
        "min_upload_chunk_size_mb": settings.min_upload_chunk_size_mb,
        "max_upload_chunk_size_mb": settings.max_upload_chunk_size_mb,
        "upload_retry_max_attempts": settings.upload_retry_max_attempts,
        "upload_retry_base_delay_seconds": settings.upload_retry_base_delay_seconds,
        "upload_retry_max_delay_seconds": settings.upload_retry_max_delay_seconds,
        "upload_connect_timeout_seconds": settings.upload_connect_timeout_seconds,
        "upload_read_timeout_padding_seconds": settings.upload_read_timeout_padding_seconds,
        "upload_min_throughput_bytes_per_second": settings.upload_min_throughput_bytes_per_second,
        "circuit_breaker_failure_threshold": settings.circuit_breaker_failure_threshold,
        "circuit_breaker_cooldown_seconds": settings.circuit_breaker_cooldown_seconds,
        "http_host": settings.http_host,
        "http_port": settings.http_port,
    }


def build_settings(payload: dict[str, Any] | None = None) -> Settings:
    raw = payload or {}
    cron_schedule = _coerce_text(raw.get("cron_schedule"), "0 2 * * *")
    CronSchedule.from_expression(cron_schedule)

    return Settings(
        edge_id=_coerce_text(raw.get("edge_id"), "edge-01"),
        scan_root=Path(_coerce_text(raw.get("scan_root"), str(_default_scan_root()))).expanduser().resolve(),
        central_url=_coerce_text(raw.get("central_url"), "http://127.0.0.1:8000").rstrip("/"),
        auth_token=str(raw.get("auth_token") or "").strip(),
        cron_schedule=cron_schedule,
        state_dir=Path(_coerce_text(raw.get("state_dir"), str(_default_state_dir()))).expanduser(),
        spool_dir=Path(_coerce_text(raw.get("spool_dir"), str(_default_spool_dir()))).expanduser(),
        log_level=_coerce_text(raw.get("log_level"), "INFO").upper(),
        max_depth=_coerce_int(raw.get("max_depth"), 10, 0),
        keep_local_pending=_coerce_bool(raw.get("keep_local_pending"), True),
        upload_chunk_size_mb=_coerce_int(raw.get("upload_chunk_size_mb"), 8, 1),
        min_upload_chunk_size_mb=_coerce_int(raw.get("min_upload_chunk_size_mb"), 1, 1),
        max_upload_chunk_size_mb=_coerce_int(raw.get("max_upload_chunk_size_mb"), 16, 1),
        upload_retry_max_attempts=_coerce_int(raw.get("upload_retry_max_attempts"), 5, 1),
        upload_retry_base_delay_seconds=_coerce_int(raw.get("upload_retry_base_delay_seconds"), 5, 1),
        upload_retry_max_delay_seconds=_coerce_int(raw.get("upload_retry_max_delay_seconds"), 300, 1),
        upload_connect_timeout_seconds=_coerce_int(raw.get("upload_connect_timeout_seconds"), 10, 1),
        upload_read_timeout_padding_seconds=_coerce_int(raw.get("upload_read_timeout_padding_seconds"), 30, 5),
        upload_min_throughput_bytes_per_second=_coerce_int(raw.get("upload_min_throughput_bytes_per_second"), 262144, 1024),
        circuit_breaker_failure_threshold=_coerce_int(raw.get("circuit_breaker_failure_threshold"), 5, 1),
        circuit_breaker_cooldown_seconds=_coerce_int(raw.get("circuit_breaker_cooldown_seconds"), 300, 1),
        http_host=_coerce_text(raw.get("http_host"), "127.0.0.1"),
        http_port=_coerce_int(raw.get("http_port"), 8080, 1),
    )


def _env_overrides() -> dict[str, Any]:
    overrides: dict[str, Any] = {}
    env_map = {
        "EDGE_ID": "edge_id",
        "SCAN_ROOT": "scan_root",
        "CENTRAL_URL": "central_url",
        "CRON_SCHEDULE": "cron_schedule",
        "STATE_DIR": "state_dir",
        "SPOOL_DIR": "spool_dir",
        "LOG_LEVEL": "log_level",
        "MAX_DEPTH": "max_depth",
        "KEEP_LOCAL_PENDING": "keep_local_pending",
        "UPLOAD_CHUNK_SIZE_MB": "upload_chunk_size_mb",
        "MIN_UPLOAD_CHUNK_SIZE_MB": "min_upload_chunk_size_mb",
        "MAX_UPLOAD_CHUNK_SIZE_MB": "max_upload_chunk_size_mb",
        "UPLOAD_RETRY_MAX_ATTEMPTS": "upload_retry_max_attempts",
        "UPLOAD_RETRY_BASE_DELAY_SECONDS": "upload_retry_base_delay_seconds",
        "UPLOAD_RETRY_MAX_DELAY_SECONDS": "upload_retry_max_delay_seconds",
        "UPLOAD_CONNECT_TIMEOUT_SECONDS": "upload_connect_timeout_seconds",
        "UPLOAD_READ_TIMEOUT_PADDING_SECONDS": "upload_read_timeout_padding_seconds",
        "UPLOAD_MIN_THROUGHPUT_BYTES_PER_SECOND": "upload_min_throughput_bytes_per_second",
        "CIRCUIT_BREAKER_FAILURE_THRESHOLD": "circuit_breaker_failure_threshold",
        "CIRCUIT_BREAKER_COOLDOWN_SECONDS": "circuit_breaker_cooldown_seconds",
        "HTTP_HOST": "http_host",
        "HTTP_PORT": "http_port",
    }
    for env_key, setting_key in env_map.items():
        value = os.getenv(env_key)
        if value is not None:
            overrides[setting_key] = value

    auth_token_file = os.getenv("AUTH_TOKEN_FILE")
    if auth_token_file:
        try:
            token_path = _resolve_auth_token_file_path(auth_token_file)
            overrides["auth_token"] = token_path.read_text(encoding="utf-8").strip()
        except OSError:
            pass
    elif os.getenv("AUTH_TOKEN"):
        overrides["auth_token"] = os.getenv("AUTH_TOKEN")

    return overrides


def load_settings() -> Settings:
    path = _settings_path()
    payload: dict[str, Any] = {}

    if path.exists():
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
            if isinstance(data, dict):
                payload = data
        except (json.JSONDecodeError, OSError):
            pass

    payload.update(_env_overrides())
    return build_settings(payload)
