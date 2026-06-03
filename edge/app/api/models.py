from __future__ import annotations

from pydantic import BaseModel, Field, field_validator


class JobConfigInput(BaseModel):
    relative_path: str
    job_name: str | None = None
    exclude: list[str] = Field(default_factory=list)
    include_hidden: bool = True
    follow_symlinks: bool = False


class EdgeSettingsInput(BaseModel):
    edge_id: str
    scan_root: str
    central_url: str
    advertised_url: str = ""
    edge_credential: str = ""
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
    ntfy_url: str = ""
    ntfy_topic: str = ""
    ntfy_message_template: str = ""
    hook_pre_command: str = ""
    hook_post_command: str = ""

    @field_validator(
        "edge_id",
        "scan_root",
        "central_url",
        "advertised_url",
        "edge_credential",
        "cron_schedule",
        "state_dir",
        "spool_dir",
        "log_level",
        "ntfy_url",
        "ntfy_topic",
        "ntfy_message_template",
        "hook_pre_command",
        "hook_post_command",
    )
    @classmethod
    def normalize_text(cls, value: str) -> str:
        return value.strip()


class EdgeNtfySettingsInput(BaseModel):
    ntfy_url: str = ""
    ntfy_topic: str = ""
    ntfy_message_template: str = ""

    @field_validator("*")
    @classmethod
    def normalize_text(cls, value: str) -> str:
        return value.strip()


class EdgeHookCommandsInput(BaseModel):
    pre_command: str = ""
    post_command: str = ""

    @field_validator("pre_command", "post_command")
    @classmethod
    def normalize_text(cls, value: str) -> str:
        return value.strip()
