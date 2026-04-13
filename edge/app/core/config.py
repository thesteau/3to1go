from __future__ import annotations

import os
import sys
from dataclasses import dataclass
from pathlib import Path

from app.core.auth import load_auth_token
from app.core.schedule import CronSchedule


APP_DIR_NAME = "RelayCentralizerEdge"


def _env_bool(name: str, default: bool) -> bool:
    raw_value = os.getenv(name)
    if raw_value is None:
        return default
    return raw_value.strip().lower() in {"1", "true", "yes", "on"}


def _default_scan_root() -> Path:
    return Path.home()


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


def _path_from_env(name: str, default: Path) -> Path:
    raw_value = os.getenv(name)
    value = raw_value.strip() if raw_value and raw_value.strip() else str(default)
    return Path(value).expanduser()


def _system_env_file() -> Path:
    if sys.platform == "win32":
        return Path(os.getenv("ProgramData", "C:/ProgramData")) / APP_DIR_NAME / "edge.env"
    if sys.platform == "darwin":
        return Path("/usr/local/etc/relaycentralizer-edge/edge.env")
    return Path("/etc/relaycentralizer-edge/edge.env")


def _load_env_file() -> None:
    candidates: list[Path] = []
    explicit_path = os.getenv("EDGE_ENV_FILE")
    if explicit_path and explicit_path.strip():
        candidates.append(Path(explicit_path.strip()).expanduser())
    else:
        if getattr(sys, "frozen", False):
            candidates.append(Path(sys.executable).resolve().parent / "edge.env")
        candidates.append(Path.cwd() / ".env")
        candidates.append(_system_env_file())
        candidates.append(_default_config_dir() / "edge.env")

    for candidate in candidates:
        if not candidate.exists() or not candidate.is_file():
            continue

        for raw_line in candidate.read_text(encoding="utf-8").splitlines():
            line = raw_line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.split("=", 1)
            normalized_key = key.strip()
            if not normalized_key or normalized_key in os.environ:
                continue
            os.environ[normalized_key] = value.strip()
        return


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



def load_settings() -> Settings:
    _load_env_file()
    cron_schedule = os.getenv("CRON_SCHEDULE", "0 2 * * *").strip()
    CronSchedule.from_expression(cron_schedule)
    config_dir = _default_config_dir()
    auth_token_file = _path_from_env("AUTH_TOKEN_FILE", config_dir / "auth_token")

    return Settings(
        edge_id=os.getenv("EDGE_ID", "edge-01").strip(),
        scan_root=_path_from_env("SCAN_ROOT", _default_scan_root()).resolve(),
        central_url=os.getenv("CENTRAL_URL", "http://127.0.0.1:8000").rstrip("/"),
        auth_token=load_auth_token(auth_token_file),
        cron_schedule=cron_schedule,
        state_dir=_path_from_env("STATE_DIR", _default_state_dir()),
        spool_dir=_path_from_env("SPOOL_DIR", _default_spool_dir()),
        log_level=os.getenv("LOG_LEVEL", "INFO").upper(),
        max_depth=max(0, int(os.getenv("MAX_DEPTH", "10"))),
        keep_local_pending=_env_bool("KEEP_LOCAL_PENDING", True),
        upload_chunk_size_mb=max(1, int(os.getenv("UPLOAD_CHUNK_SIZE_MB", "8"))),
        min_upload_chunk_size_mb=max(1, int(os.getenv("MIN_UPLOAD_CHUNK_SIZE_MB", "1"))),
        max_upload_chunk_size_mb=max(1, int(os.getenv("MAX_UPLOAD_CHUNK_SIZE_MB", "16"))),
        upload_retry_max_attempts=max(1, int(os.getenv("UPLOAD_RETRY_MAX_ATTEMPTS", "5"))),
        upload_retry_base_delay_seconds=max(1, int(os.getenv("UPLOAD_RETRY_BASE_DELAY_SECONDS", "5"))),
        upload_retry_max_delay_seconds=max(1, int(os.getenv("UPLOAD_RETRY_MAX_DELAY_SECONDS", "300"))),
        upload_connect_timeout_seconds=max(1, int(os.getenv("UPLOAD_CONNECT_TIMEOUT_SECONDS", "10"))),
        upload_read_timeout_padding_seconds=max(5, int(os.getenv("UPLOAD_READ_TIMEOUT_PADDING_SECONDS", "30"))),
        upload_min_throughput_bytes_per_second=max(1024, int(os.getenv("UPLOAD_MIN_THROUGHPUT_BYTES_PER_SECOND", "262144"))),
        circuit_breaker_failure_threshold=max(1, int(os.getenv("CIRCUIT_BREAKER_FAILURE_THRESHOLD", "5"))),
        circuit_breaker_cooldown_seconds=max(1, int(os.getenv("CIRCUIT_BREAKER_COOLDOWN_SECONDS", "300"))),
        http_host=os.getenv("HTTP_HOST", "127.0.0.1"),
        http_port=max(1, int(os.getenv("HTTP_PORT", "8080"))),
    )
