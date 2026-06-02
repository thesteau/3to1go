from __future__ import annotations

import shutil
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException
from fastapi.requests import Request
from fastapi.responses import HTMLResponse

from app.api.dependencies import get_ingest_service, get_settings, get_settings_store, get_snapshot_index, get_storage_backend
from app.api.models import CentralSettingsInput, HealthResponse
from app.api.views import templates
from app.core.config import Settings
from app.index.base import SnapshotIndexBackend
from app.services.ingest import IngestService
from app.services.overview import build_overview
from app.services.settings_store import SettingsStore
from app.storage.local import LocalFilesystemBackend
from app.utils.paths import validate_namespace_component


router = APIRouter()


@router.get("/", response_class=HTMLResponse)
async def ui(request: Request) -> HTMLResponse:
    return templates.TemplateResponse("index.html", {"request": request, "title": "RelayCentralizer Central"})


@router.get("/api/overview")
async def overview(
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
    snapshot_index: SnapshotIndexBackend = Depends(get_snapshot_index),
) -> dict:
    return build_overview(settings, storage_backend, snapshot_index)


@router.post("/api/settings")
async def save_settings(
    config: CentralSettingsInput,
    request: Request,
    settings: Settings = Depends(get_settings),
    settings_store: SettingsStore = Depends(get_settings_store),
) -> dict:
    try:
        saved_settings = settings_store.save({**settings_store.snapshot(settings), **config.model_dump()})
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    request.app.state.apply_settings(saved_settings)
    await request.app.state.restart_cleanup_task(saved_settings.upload_cleanup_interval_seconds)
    return {
        "status": "ok",
        "settings": build_overview(
            request.app.state.settings,
            request.app.state.storage_backend,
            request.app.state.snapshot_index,
        )["settings"],
    }


@router.delete("/api/instances/{edge_id}/{edge_instance_id}")
async def delete_instance(
    edge_id: str,
    edge_instance_id: str,
    cleanup_missing: bool = False,
    settings: Settings = Depends(get_settings),
    ingest_service: IngestService = Depends(get_ingest_service),
    snapshot_index: SnapshotIndexBackend = Depends(get_snapshot_index),
) -> dict:
    try:
        validate_namespace_component(edge_id, "edge_id")
        validate_namespace_component(edge_instance_id, "edge_instance_id")
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    instance_dir = settings.backup_root / edge_id / edge_instance_id
    if not instance_dir.exists():
        if snapshot_index.get_edge_registration(edge_id, edge_instance_id) is not None:
            if cleanup_missing:
                snapshot_index.delete_edge_registration(edge_id, edge_instance_id)
                return {"status": "cleaned", "edge_id": edge_id, "edge_instance_id": edge_instance_id}
            raise HTTPException(
                status_code=409,
                detail={
                    "message": "instance files not found",
                    "cleanup_available": True,
                },
            )
        raise HTTPException(status_code=404, detail="instance not found")

    # Collect job namespaces before deleting so we can reconcile the index
    job_namespaces = [
        f"{edge_id}/{edge_instance_id}/{child.name}"
        for child in instance_dir.iterdir()
        if child.is_dir()
    ]

    shutil.rmtree(instance_dir, ignore_errors=True)

    for namespace in job_namespaces:
        ingest_service.reconcile_namespace(namespace)
    if instance_dir.exists() or _instance_has_index_entries(snapshot_index, edge_id, edge_instance_id):
        raise HTTPException(
            status_code=409,
            detail={
                "message": "instance still has backup files or index entries",
                "cleanup_available": False,
            },
        )
    snapshot_index.delete_edge_registration(edge_id, edge_instance_id)

    return {"status": "deleted", "edge_id": edge_id, "edge_instance_id": edge_instance_id}


@router.get("/health/ready")
async def health_ready(
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
) -> dict:
    if not storage_backend.healthcheck():
        raise HTTPException(status_code=503, detail="storage backend unavailable")
    settings.staging_dir.mkdir(parents=True, exist_ok=True)
    return {"status": "ok"}


def _directory_usage(path: Path) -> int:
    total = 0
    if not path.exists():
        return 0
    for entry in path.rglob("*"):
        if entry.is_file():
            try:
                total += entry.stat().st_size
            except OSError:
                continue
    return total


def _instance_has_index_entries(
    snapshot_index: SnapshotIndexBackend,
    edge_id: str,
    edge_instance_id: str,
) -> bool:
    for namespace in snapshot_index.list_namespaces():
        if namespace.get("edge_id") == edge_id and namespace.get("edge_instance_id") == edge_instance_id:
            return True
    return False


def _disk_usage(path: Path) -> tuple[int, int, int]:
    target = path
    while not target.exists() and target != target.parent:
        target = target.parent
    usage = shutil.disk_usage(target)
    return usage.total, usage.used, usage.free


@router.get(
    "/health",
    response_model=HealthResponse,
)
async def health(
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
) -> HealthResponse:
    if not storage_backend.healthcheck():
        raise HTTPException(status_code=503, detail="storage backend unavailable")

    _, _, staging_free = _disk_usage(settings.staging_dir)
    staging_used_bytes = _directory_usage(settings.staging_dir)

    _, _, backup_free = _disk_usage(settings.backup_root)
    backup_used_bytes = _directory_usage(settings.backup_root)

    return HealthResponse(
        status="ok",
        staging_dir=str(settings.staging_dir),
        staging_used_bytes=staging_used_bytes,
        staging_free_bytes=staging_free,
        backup_root=str(settings.backup_root),
        backup_used_bytes=backup_used_bytes,
        backup_free_bytes=backup_free,
        max_upload_size_bytes=settings.max_upload_size_bytes,
        recommended_chunk_size_bytes=settings.upload_chunk_size_bytes,
    )
