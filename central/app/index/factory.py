from __future__ import annotations

from app.core.config import Settings
from app.index.base import SnapshotIndexBackend
from app.index.file import FileSnapshotIndexBackend
from app.index.postgres import PostgresSnapshotIndexBackend


def build_snapshot_index_backend(settings: Settings) -> SnapshotIndexBackend:
    if settings.index_database_url:
        return PostgresSnapshotIndexBackend(settings.index_database_url)
    return FileSnapshotIndexBackend(settings.backup_root)
