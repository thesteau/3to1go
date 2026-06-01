from __future__ import annotations

import asyncio
import logging
from datetime import timedelta

from fastapi import FastAPI, HTTPException
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles

from app.api.routes_automation import router as automation_router
from app.api.routes_overview import router as overview_router
from app.api.routes_snapshots import router as snapshots_router
from app.api.routes_uploads import router as uploads_router
from app.api.views import STATIC_DIR
from app.core.config import Settings, hook_scripts_dir, load_settings
from app.core.logging import configure_logging
from app.index.factory import build_snapshot_index_backend
from app.services.hooks import HookManager
from app.services.ingest import IngestService
from app.services.locks import NamespaceLockManager
from app.services.ntfy import NtfyPublisher
from app.services.settings_store import SettingsStore
from app.storage.factory import build_storage_backend


def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or load_settings()
    logger = configure_logging(settings.log_level)
    storage_backend = build_storage_backend(settings)
    snapshot_index = build_snapshot_index_backend(settings)
    settings_store = SettingsStore()
    hook_manager = HookManager(hook_scripts_dir(), logger)
    ntfy_publisher = NtfyPublisher(logger)
    ingest_service = IngestService(
        settings=settings,
        storage_backend=storage_backend,
        snapshot_index=snapshot_index,
        lock_manager=NamespaceLockManager(),
        staging_dir=settings.staging_dir,
        max_upload_size_bytes=settings.max_upload_size_bytes,
        recommended_chunk_size_bytes=settings.upload_chunk_size_bytes,
        upload_session_ttl_hours=settings.upload_session_ttl_hours,
        retention_keep_last=settings.retention_keep_last,
        logger=logger,
        hook_manager=hook_manager,
        ntfy_publisher=ntfy_publisher,
    )

    app = FastAPI(title="RelayCentralizer Central", version="0.1.0")
    app.mount("/static", StaticFiles(directory=str(STATIC_DIR)), name="static")
    app.state.settings = settings
    app.state.logger = logger
    app.state.storage_backend = storage_backend
    app.state.snapshot_index = snapshot_index
    app.state.settings_store = settings_store
    app.state.ingest_service = ingest_service
    app.state.hook_manager = hook_manager
    app.state.ntfy_publisher = ntfy_publisher
    app.state.cleanup_task = None
    app.include_router(overview_router)
    app.include_router(automation_router)
    app.include_router(snapshots_router)
    app.include_router(uploads_router)

    async def restart_cleanup_task(interval_seconds: int) -> None:
        cleanup_task = app.state.cleanup_task
        if cleanup_task is not None:
            cleanup_task.cancel()
            try:
                await cleanup_task
            except asyncio.CancelledError:
                pass
        app.state.cleanup_task = asyncio.create_task(
            ingest_service.cleanup_loop(interval_seconds)
        )

    def apply_settings(new_settings: Settings) -> None:
        app.state.settings = new_settings
        ingest_service.settings = new_settings
        ingest_service.retention_keep_last = new_settings.retention_keep_last
        ingest_service.max_upload_size_bytes = new_settings.max_upload_size_bytes
        ingest_service.recommended_chunk_size_bytes = new_settings.upload_chunk_size_bytes
        ingest_service.upload_session_ttl = timedelta(hours=new_settings.upload_session_ttl_hours)
        logger.setLevel(getattr(logging, new_settings.log_level, logging.INFO))

    app.state.restart_cleanup_task = restart_cleanup_task
    app.state.apply_settings = apply_settings

    @app.on_event("startup")
    async def on_startup() -> None:
        await restart_cleanup_task(settings.upload_cleanup_interval_seconds)

    @app.on_event("shutdown")
    async def on_shutdown() -> None:
        cleanup_task = app.state.cleanup_task
        if cleanup_task is not None:
            cleanup_task.cancel()
            try:
                await cleanup_task
            except asyncio.CancelledError:
                pass

    @app.exception_handler(HTTPException)
    async def http_exception_handler(_, exc: HTTPException) -> JSONResponse:
        return JSONResponse(status_code=exc.status_code, content={"detail": exc.detail})

    return app
