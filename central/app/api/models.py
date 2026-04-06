from __future__ import annotations

from datetime import datetime

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
    job_name: str = Field(min_length=1, max_length=128)
    fingerprint: str = Field(min_length=8, max_length=128)
    timestamp: str = Field(min_length=1, max_length=64)
    archive_format: str

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


class UploadInitRequest(BaseModel):
    edge_id: str = Field(min_length=1, max_length=128)
    job_name: str = Field(min_length=1, max_length=128)
    fingerprint: str = Field(min_length=8, max_length=128)
    timestamp: str = Field(min_length=1, max_length=64)
    archive_format: str
    archive_size_bytes: int = Field(gt=0)
    archive_sha256: str = Field(min_length=64, max_length=64)
    idempotency_key: str = Field(min_length=8, max_length=256)

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
