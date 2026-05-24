from __future__ import annotations

from fastapi import APIRouter, Depends, Header, HTTPException
from fastapi.responses import FileResponse

from app.api.auth import authorize_request
from app.api.dependencies import get_logger, get_settings, get_storage_backend
from app.core.config import Settings
from app.storage.local import LocalFilesystemBackend
from app.utils.paths import validate_namespace_component


router = APIRouter()


@router.get("/api/snapshots/{edge_id}/{job_name}/{filename}")
async def download_snapshot(
    edge_id: str,
    job_name: str,
    filename: str,
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
) -> FileResponse:
    try:
        validate_namespace_component(edge_id, "edge_id")
        validate_namespace_component(job_name, "job_name")
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    target = settings.backup_root / edge_id / job_name / filename
    try:
        target.resolve().relative_to(settings.backup_root.resolve())
    except ValueError:
        raise HTTPException(status_code=400, detail="invalid path")

    if not target.is_file():
        raise HTTPException(status_code=404, detail="snapshot not found")

    return FileResponse(str(target), filename=filename, media_type="application/octet-stream")


@router.delete("/api/snapshots/{edge_id}/{job_name}/{filename}")
async def delete_snapshot(
    edge_id: str,
    job_name: str,
    filename: str,
    authorization: str | None = Header(default=None),
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
    logger=Depends(get_logger),
) -> dict:
    authorize_request(authorization, settings, logger)

    try:
        validate_namespace_component(edge_id, "edge_id")
        validate_namespace_component(job_name, "job_name")
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    target = settings.backup_root / edge_id / job_name / filename
    try:
        target.resolve().relative_to(settings.backup_root.resolve())
    except ValueError:
        raise HTTPException(status_code=400, detail="invalid path")

    if not target.is_file():
        raise HTTPException(status_code=404, detail="snapshot not found")

    storage_backend.delete(f"{edge_id}/{job_name}", filename)
    return {"status": "deleted", "filename": filename}
