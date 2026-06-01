from __future__ import annotations

from datetime import datetime
from urllib.parse import urlparse

from pydantic import BaseModel, Field, field_validator


class HealthResponse(BaseModel):
    status: str = "ok"
    staging_dir: str
    staging_used_bytes: int
    staging_free_bytes: int
    backup_root: str
    backup_used_bytes: int
    backup_free_bytes: int
    max_upload_size_bytes: int
    recommended_chunk_size_bytes: int


class UploadResponse(BaseModel):
    status: str = "ok"
    stored_as: str
    pruned: int
    duplicate: bool = False


class UploadMetadata(BaseModel):
    edge_id: str = Field(min_length=1, max_length=128)
    edge_instance_id: str | None = Field(default=None, min_length=8, max_length=128)
    job_name: str = Field(min_length=1, max_length=128)
    fingerprint: str = Field(min_length=8, max_length=128)
    timestamp: str = Field(min_length=1, max_length=64)
    archive_format: str
    encryption_key_fingerprint: str | None = Field(default=None, min_length=16, max_length=128)
    advertised_url: str | None = Field(default=None, max_length=512)

    @field_validator("archive_format")
    @classmethod
    def validate_archive_format(cls, value: str) -> str:
        normalized = value.strip().lower()
        if normalized != "tar.zst":
            raise ValueError("archive_format must be tar.zst")
        return normalized

    @field_validator("edge_instance_id", "encryption_key_fingerprint")
    @classmethod
    def normalize_optional_text(cls, value: str | None) -> str | None:
        if value is None:
            return None
        normalized = value.strip().lower()
        return normalized or None

    @field_validator("advertised_url")
    @classmethod
    def normalize_optional_url(cls, value: str | None) -> str | None:
        return _normalize_optional_url(value)

    @field_validator("timestamp")
    @classmethod
    def validate_timestamp(cls, value: str) -> str:
        normalized = value.strip()
        datetime.strptime(normalized, "%Y-%m-%dT%H:%M:%SZ")
        return normalized


class UploadInitRequest(BaseModel):
    edge_id: str = Field(min_length=1, max_length=128)
    edge_instance_id: str | None = Field(default=None, min_length=8, max_length=128)
    job_name: str = Field(min_length=1, max_length=128)
    fingerprint: str = Field(min_length=8, max_length=128)
    timestamp: str = Field(min_length=1, max_length=64)
    archive_format: str
    archive_size_bytes: int = Field(gt=0)
    archive_sha256: str = Field(min_length=64, max_length=64)
    idempotency_key: str = Field(min_length=8, max_length=256)
    encryption_key_fingerprint: str | None = Field(default=None, min_length=16, max_length=128)
    advertised_url: str | None = Field(default=None, max_length=512)

    @field_validator("archive_format")
    @classmethod
    def validate_archive_format(cls, value: str) -> str:
        normalized = value.strip().lower()
        if normalized != "tar.zst":
            raise ValueError("archive_format must be tar.zst")
        return normalized

    @field_validator("timestamp")
    @classmethod
    def validate_timestamp(cls, value: str) -> str:
        normalized = value.strip()
        datetime.strptime(normalized, "%Y-%m-%dT%H:%M:%SZ")
        return normalized

    @field_validator("archive_sha256")
    @classmethod
    def validate_archive_sha256(cls, value: str) -> str:
        normalized = value.strip().lower()
        if len(normalized) != 64 or any(ch not in "0123456789abcdef" for ch in normalized):
            raise ValueError("archive_sha256 must be a 64-character lowercase hex digest")
        return normalized

    @field_validator("edge_instance_id", "encryption_key_fingerprint")
    @classmethod
    def normalize_optional_text(cls, value: str | None) -> str | None:
        if value is None:
            return None
        normalized = value.strip().lower()
        return normalized or None

    @field_validator("advertised_url")
    @classmethod
    def normalize_optional_url(cls, value: str | None) -> str | None:
        return _normalize_optional_url(value)


class UploadSessionResponse(BaseModel):
    upload_id: str
    status: str
    next_offset: int
    archive_size_bytes: int
    recommended_chunk_size_bytes: int
    stored_as: str | None = None
    pruned: int = 0
    duplicate: bool = False


class UploadChunkResponse(BaseModel):
    upload_id: str
    status: str
    next_offset: int
    received_bytes: int


class CentralSettingsInput(BaseModel):
    retention_keep_last: int = Field(ge=1)
    log_level: str = Field(min_length=4, max_length=16)
    max_upload_size_mb: int = Field(ge=1)
    upload_chunk_size_mb: int = Field(ge=1)
    upload_session_ttl_hours: int = Field(ge=1)
    upload_cleanup_interval_seconds: int = Field(ge=10)
    ntfy_url: str = ""
    ntfy_topic: str = ""
    ntfy_message_template: str = ""
    ntfy_match_edge_id: str = ""
    ntfy_match_edge_instance_id: str = ""
    ntfy_match_source: str = ""
    hook_pre_command: str = ""
    hook_post_command: str = ""

    @field_validator("log_level")
    @classmethod
    def normalize_log_level(cls, value: str) -> str:
        normalized = value.strip().upper()
        if normalized not in {"DEBUG", "INFO", "WARNING", "ERROR"}:
            raise ValueError("log_level must be one of DEBUG, INFO, WARNING, ERROR")
        return normalized

    @field_validator(
        "ntfy_url",
        "ntfy_topic",
        "ntfy_message_template",
        "ntfy_match_edge_id",
        "ntfy_match_edge_instance_id",
        "ntfy_match_source",
        "hook_pre_command",
        "hook_post_command",
    )
    @classmethod
    def normalize_optional_text(cls, value: str) -> str:
        return value.strip()


class CentralNtfySettingsInput(BaseModel):
    ntfy_url: str = ""
    ntfy_topic: str = ""
    ntfy_message_template: str = ""
    ntfy_match_edge_id: str = ""
    ntfy_match_edge_instance_id: str = ""
    ntfy_match_source: str = ""

    @field_validator("*")
    @classmethod
    def normalize_text(cls, value: str) -> str:
        return value.strip()


class CentralHookCommandsInput(BaseModel):
    pre_command: str = ""
    post_command: str = ""

    @field_validator("pre_command", "post_command")
    @classmethod
    def normalize_text(cls, value: str) -> str:
        return value.strip()


def _normalize_optional_url(value: str | None) -> str | None:
    if value is None:
        return None
    normalized = value.strip().rstrip("/")
    if not normalized:
        return None
    parsed = urlparse(normalized)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise ValueError("advertised_url must be a full http or https URL")
    return normalized
