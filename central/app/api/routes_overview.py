from __future__ import annotations

import shutil
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException
from fastapi.requests import Request
from fastapi.responses import HTMLResponse

from app.api.dependencies import get_settings, get_storage_backend
from app.api.models import HealthResponse
from app.api.views import templates
from app.core.config import Settings
from app.services.overview import build_overview
from app.storage.local import LocalFilesystemBackend


router = APIRouter()


@router.get("/", response_class=HTMLResponse)
async def ui(request: Request) -> HTMLResponse:
    return templates.TemplateResponse("index.html", {"request": request, "title": "RelayCentralizer Central"})


@router.get("/api/overview")
async def overview(
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
) -> dict:
    return build_overview(settings, storage_backend)


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


def _disk_usage(path: Path) -> tuple[int, int, int]:
    path.mkdir(parents=True, exist_ok=True)
    usage = shutil.disk_usage(path)
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
    )
