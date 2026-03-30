from __future__ import annotations

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


@router.get("/health", response_model=HealthResponse)
async def health(storage_backend: LocalFilesystemBackend = Depends(get_storage_backend)) -> HealthResponse:
    if not storage_backend.healthcheck():
        raise HTTPException(status_code=503, detail="storage backend unavailable")
    return HealthResponse(status="ok")
