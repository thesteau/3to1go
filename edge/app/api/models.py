from __future__ import annotations

from pydantic import BaseModel, Field


class JobConfigInput(BaseModel):
    relative_path: str
    job_name: str | None = None
    exclude: list[str] = Field(default_factory=list)
    include_hidden: bool = True
    follow_symlinks: bool = False
    is_docker_composed: bool = False
    update_container_on_packup: bool = False


class EdgeSettingsInput(BaseModel):
    edge_id: str
    scan_root: str
    central_url: str
    auth_token: str = ""
    cron_schedule: str
    state_dir: str
    spool_dir: str
    log_level: str
    max_depth: int
    keep_local_pending: bool = True
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
