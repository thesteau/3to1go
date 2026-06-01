from __future__ import annotations

from fastapi import APIRouter, Depends, Header, HTTPException
from fastapi.responses import FileResponse

from app.api.auth import authorize_request
from app.api.dependencies import get_ingest_service, get_logger, get_settings, get_storage_backend
from app.core.config import Settings
from app.services.ingest import IngestService
from app.storage.local import LocalFilesystemBackend
from app.utils.paths import validate_namespace_component


router = APIRouter()


def _validated_namespace(edge_id: str, job_name: str, edge_instance_id: str | None = None) -> str:
    validate_namespace_component(edge_id, "edge_id")
    validate_namespace_component(job_name, "job_name")
    if edge_instance_id is None:
        return f"{edge_id}/{job_name}"
    validate_namespace_component(edge_instance_id, "edge_instance_id")
    return f"{edge_id}/{edge_instance_id}/{job_name}"


def _validated_target(settings: Settings, namespace: str, filename: str):
    target = settings.backup_root / namespace / filename
    try:
        target.resolve().relative_to(settings.backup_root.resolve())
    except ValueError as exc:
        raise HTTPException(status_code=400, detail="invalid path") from exc
    return target


def _latest_snapshot(storage_backend: LocalFilesystemBackend, namespace: str) -> dict | None:
    snapshots = storage_backend.list(namespace)
    if not snapshots:
        return None
    return max(snapshots, key=lambda item: (float(item.get("mtime", 0)), str(item.get("filename", ""))))


@router.get("/backup/recovery/{edge_id}/{edge_instance_id}/{job_name}/latest")
async def download_latest_snapshot_for_recovery(
    edge_id: str,
    edge_instance_id: str,
    job_name: str,
    authorization: str | None = Header(default=None),
    settings: Settings = Depends(get_settings),
    logger=Depends(get_logger),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
) -> FileResponse:
    authorize_request(authorization, settings, logger)

    try:
        namespace = _validated_namespace(edge_id, job_name, edge_instance_id)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    latest = _latest_snapshot(storage_backend, namespace)
    if latest is None:
        raise HTTPException(status_code=404, detail="no snapshots found")

    filename = str(latest["filename"])
    target = _validated_target(settings, namespace, filename)
    if not target.is_file():
        raise HTTPException(status_code=404, detail="snapshot not found")

    return FileResponse(
        str(target),
        filename=filename,
        media_type="application/octet-stream",
        headers={"X-Relay-Snapshot-Filename": filename},
    )


@router.get("/api/snapshots/{edge_id}/{edge_instance_id}/{job_name}/{filename}")
async def download_snapshot_for_instance(
    edge_id: str,
    edge_instance_id: str,
    job_name: str,
    filename: str,
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
) -> FileResponse:
    try:
        namespace = _validated_namespace(edge_id, job_name, edge_instance_id)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    target = _validated_target(settings, namespace, filename)

    if not target.is_file():
        raise HTTPException(status_code=404, detail="snapshot not found")

    return FileResponse(str(target), filename=filename, media_type="application/octet-stream")


@router.get("/api/snapshots/{edge_id}/{job_name}/{filename}")
async def download_snapshot(
    edge_id: str,
    job_name: str,
    filename: str,
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
) -> FileResponse:
    try:
        namespace = _validated_namespace(edge_id, job_name)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    target = _validated_target(settings, namespace, filename)

    if not target.is_file():
        raise HTTPException(status_code=404, detail="snapshot not found")

    return FileResponse(str(target), filename=filename, media_type="application/octet-stream")


@router.delete("/api/snapshots/{edge_id}/{edge_instance_id}/{job_name}/{filename}")
async def delete_snapshot_for_instance(
    edge_id: str,
    edge_instance_id: str,
    job_name: str,
    filename: str,
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
    ingest_service: IngestService = Depends(get_ingest_service),
) -> dict:
    try:
        namespace = _validated_namespace(edge_id, job_name, edge_instance_id)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    target = _validated_target(settings, namespace, filename)

    if not target.is_file():
        raise HTTPException(status_code=404, detail="snapshot not found")

    storage_backend.delete(namespace, filename)
    ingest_service.reconcile_namespace(namespace)
    return {"status": "deleted", "filename": filename}


@router.delete("/api/snapshots/{edge_id}/{job_name}/{filename}")
async def delete_snapshot(
    edge_id: str,
    job_name: str,
    filename: str,
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
    ingest_service: IngestService = Depends(get_ingest_service),
) -> dict:
    try:
        namespace = _validated_namespace(edge_id, job_name)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    target = _validated_target(settings, namespace, filename)

    if not target.is_file():
        raise HTTPException(status_code=404, detail="snapshot not found")

    storage_backend.delete(namespace, filename)
    ingest_service.reconcile_namespace(namespace)
    return {"status": "deleted", "filename": filename}
