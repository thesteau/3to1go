from __future__ import annotations

from app.core.config import Settings
from app.services.ingest import IngestService
from app.storage.local import LocalFilesystemBackend


def build_overview(settings: Settings, storage_backend: LocalFilesystemBackend, ingest_service: IngestService) -> dict:
    namespaces: list[dict] = []
    if settings.backup_root.exists():
        edge_dirs = sorted(
            [item for item in settings.backup_root.iterdir() if item.is_dir() and not item.name.startswith(".")],
            key=lambda item: item.name.lower(),
        )
        for edge_dir in edge_dirs:
            registration = ingest_service.get_edge_registration(edge_dir.name) or {}
            jobs: list[dict] = []
            job_dirs = sorted(
                [item for item in edge_dir.iterdir() if item.is_dir()],
                key=lambda item: item.name.lower(),
            )
            for job_dir in job_dirs:
                namespace = f"{edge_dir.name}/{job_dir.name}"
                items = sorted(storage_backend.list(namespace), key=lambda x: x["mtime"], reverse=True)
                snapshots = [
                    {"name": item["filename"], "size_bytes": item.get("size_bytes", 0), "mtime": item["mtime"]}
                    for item in items
                ]
                jobs.append({"job_name": job_dir.name, "snapshot_count": len(snapshots), "snapshots": snapshots})
            namespaces.append(
                {
                    "edge_id": edge_dir.name,
                    "edge_instance_id": registration.get("edge_instance_id"),
                    "encryption_key_fingerprint": registration.get("encryption_key_fingerprint"),
                    "first_seen_at": registration.get("first_seen_at"),
                    "last_seen_at": registration.get("last_seen_at"),
                    "jobs": jobs,
                }
            )

    return {
        "status": "ok" if storage_backend.healthcheck() else "degraded",
        "backup_root": str(settings.backup_root),
        "staging_dir": str(settings.staging_dir),
        "retention_keep_last": settings.retention_keep_last,
        "http_url": f"http://localhost:{settings.http_port}",
        "namespaces": namespaces,
    }
