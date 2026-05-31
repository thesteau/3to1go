from __future__ import annotations

from fastapi import Request

from app.core.config import Settings
from app.index.base import SnapshotIndexBackend
from app.services.ingest import IngestService
from app.services.settings_store import SettingsStore
from app.storage.local import LocalFilesystemBackend


def get_settings(request: Request) -> Settings:
    return request.app.state.settings


def get_logger(request: Request):
    return request.app.state.logger


def get_storage_backend(request: Request) -> LocalFilesystemBackend:
    return request.app.state.storage_backend


def get_snapshot_index(request: Request) -> SnapshotIndexBackend:
    return request.app.state.snapshot_index


def get_settings_store(request: Request) -> SettingsStore:
    return request.app.state.settings_store


def get_ingest_service(request: Request) -> IngestService:
    return request.app.state.ingest_service
