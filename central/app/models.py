from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel, Field, field_validator


class HealthResponse(BaseModel):
    status: str = "ok"


class UploadResponse(BaseModel):
    status: str = "ok"
    stored_as: str
    pruned: int


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
