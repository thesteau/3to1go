from __future__ import annotations

from fastapi import Request

from app.core.config import Settings
from app.services.ingest import IngestService
from app.storage.local import LocalFilesystemBackend


def get_settings(request: Request) -> Settings:
    return request.app.state.settings


def get_logger(request: Request):
    return request.app.state.logger


def get_storage_backend(request: Request) -> LocalFilesystemBackend:
    return request.app.state.storage_backend


def get_ingest_service(request: Request) -> IngestService:
    return request.app.state.ingest_service
