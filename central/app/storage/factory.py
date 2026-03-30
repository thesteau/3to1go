from __future__ import annotations

from app.core.config import Settings
from app.storage.local import LocalFilesystemBackend


def build_storage_backend(settings: Settings) -> LocalFilesystemBackend:
    if settings.storage_backend != "local":
        raise RuntimeError(f"unsupported storage backend: {settings.storage_backend}")
    return LocalFilesystemBackend(settings.backup_root)
