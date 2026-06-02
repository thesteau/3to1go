from __future__ import annotations

import asyncio
import json
import shutil
from pathlib import Path
from urllib import error, parse, request

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


router = APIRouter()


@router.get("/", response_class=HTMLResponse)
async def ui(request: Request) -> HTMLResponse:
    return templates.TemplateResponse("index.html", {"request": request, "title": "RelayCentralizer Central"})


@router.get("/api/overview")
async def overview(
    settings: Settings = Depends(get_settings),
    storage_backend: LocalFilesystemBackend = Depends(get_storage_backend),
    ingest_service: IngestService = Depends(get_ingest_service),
    snapshot_index: SnapshotIndexBackend = Depends(get_snapshot_index),
) -> dict:
    return build_overview(settings, storage_backend, ingest_service, snapshot_index)


@router.post("/api/edges/{edge_id}/{edge_instance_id}/jobs/{job_name}/force-send")
async def force_send_edge_job(
    edge_id: str,
    edge_instance_id: str,
    job_name: str,
    snapshot_index: SnapshotIndexBackend = Depends(get_snapshot_index),
) -> dict:
    registration = snapshot_index.get_edge_registration(edge_id, edge_instance_id)
    if registration is None:
        raise HTTPException(status_code=404, detail="edge registration not found")

    advertised_url = str(registration.get("advertised_url") or "").strip()
    if not advertised_url:
        raise HTTPException(status_code=400, detail="edge does not advertise a reachable URL")

    target_url = (
        f"{advertised_url.rstrip('/')}/api/jobs/force-send"
        f"?job_name={parse.quote(job_name, safe='')}"
    )
    req = request.Request(target_url, data=b"", method="POST")

    def _do_request() -> dict:
        try:
            with request.urlopen(req, timeout=10) as response:
                payload = response.read().decode("utf-8", errors="replace")
                body = json.loads(payload) if payload else {}
                if response.status >= 400:
                    raise HTTPException(status_code=response.status, detail=body.get("detail") or "edge force send failed")
                return body
        except error.HTTPError as exc:
            payload = exc.read().decode("utf-8", errors="replace")
            detail = ""
            if payload:
                try:
                    detail = json.loads(payload).get("detail") or ""
                except json.JSONDecodeError:
                    detail = payload
            raise HTTPException(status_code=exc.code, detail=detail or f"edge returned {exc.code}") from exc
        except error.URLError as exc:
            raise HTTPException(status_code=502, detail=str(exc.reason) or "unable to reach edge") from exc

    return await asyncio.to_thread(_do_request)


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
            request.app.state.ingest_service,
            request.app.state.snapshot_index,
        )["settings"],
    }


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
