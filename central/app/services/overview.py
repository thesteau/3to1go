from __future__ import annotations

from app.core.config import Settings, settings_storage_path, settings_to_payload
from app.index.base import SnapshotIndexBackend
from app.services.ingest import IngestService
from app.storage.local import LocalFilesystemBackend


def build_overview(
    settings: Settings,
    storage_backend: LocalFilesystemBackend,
    ingest_service: IngestService,
    snapshot_index: SnapshotIndexBackend,
) -> dict:
    namespaces: list[dict] = []
    for namespace in snapshot_index.list_namespaces():
        registration = ingest_service.get_edge_registration(namespace["edge_id"]) or {}
        namespaces.append(
            {
                "edge_id": namespace["edge_id"],
                "edge_instance_id": registration.get("edge_instance_id"),
                "encryption_key_fingerprint": registration.get("encryption_key_fingerprint"),
                "first_seen_at": registration.get("first_seen_at"),
                "last_seen_at": registration.get("last_seen_at"),
                "last_seen_source": registration.get("last_seen_source"),
                "jobs": namespace["jobs"],
            }
        )

    return {
        "status": "ok" if storage_backend.healthcheck() else "degraded",
        "backup_root": str(settings.backup_root),
        "staging_dir": str(settings.staging_dir),
        "retention_keep_last": settings.retention_keep_last,
        "settings_path": str(settings_storage_path()),
        "settings": settings_to_payload(settings),
        "http_url": f"http://localhost:{settings.http_port}",
        "namespaces": namespaces,
    }
