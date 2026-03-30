from __future__ import annotations

from app.core.config import Settings
from app.storage.local import LocalFilesystemBackend


def build_overview(settings: Settings, storage_backend: LocalFilesystemBackend) -> dict:
    namespaces: list[dict] = []
    if settings.backup_root.exists():
        for edge_dir in sorted([item for item in settings.backup_root.iterdir() if item.is_dir()], key=lambda item: item.name.lower()):
            jobs: list[dict] = []
            for job_dir in sorted([item for item in edge_dir.iterdir() if item.is_dir()], key=lambda item: item.name.lower()):
                snapshots = sorted([item.name for item in job_dir.iterdir() if item.is_file()], reverse=True)
                jobs.append({"job_name": job_dir.name, "snapshot_count": len(snapshots), "snapshots": snapshots})
            namespaces.append({"edge_id": edge_dir.name, "jobs": jobs})

    return {
        "status": "ok" if storage_backend.healthcheck() else "degraded",
        "backup_root": str(settings.backup_root),
        "staging_dir": str(settings.staging_dir),
        "retention_keep_last": settings.retention_keep_last,
        "http_url": f"http://localhost:{settings.http_port}",
        "namespaces": namespaces,
    }
