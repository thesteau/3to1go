from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from app.core.auth import load_auth_token
from app.core.schedule import CronSchedule


def _env_bool(name: str, default: bool) -> bool:
    raw_value = os.getenv(name)
    if raw_value is None:
        return default
    return raw_value.strip().lower() in {"1", "true", "yes", "on"}


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
    cron_schedule = os.getenv("CRON_SCHEDULE", "0 2 * * *").strip()
    CronSchedule.from_expression(cron_schedule)

    return Settings(
        edge_id=os.getenv("EDGE_ID", "edge-01").strip(),
        scan_root=Path(os.getenv("SCAN_ROOT", "/scan")).resolve(),
        central_url=os.getenv("CENTRAL_URL", "http://central:8000").rstrip("/"),
        auth_token=load_auth_token(),
        cron_schedule=cron_schedule,
        state_dir=Path(os.getenv("STATE_DIR", "/data/state")),
        spool_dir=Path(os.getenv("SPOOL_DIR", "/data/spool")),
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
        http_host=os.getenv("HTTP_HOST", "0.0.0.0"),
        http_port=max(1, int(os.getenv("HTTP_PORT", "8080"))),
    )
