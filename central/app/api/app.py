from __future__ import annotations

import asyncio
import logging
from datetime import timedelta
from pathlib import Path

from fastapi import FastAPI, HTTPException
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles

from app.api.routes_admin import router as admin_router
from app.api.routes_automation import router as automation_router
from app.api.routes_overview import router as overview_router
from app.api.routes_snapshots import router as snapshots_router
from app.api.routes_uploads import router as uploads_router
from app.api.views import STATIC_DIR
from app.core.config import Settings, app_database_path, hook_scripts_dir, load_settings
from app.core.logging import configure_logging
from app.index.factory import build_snapshot_index_backend
from app.services.hooks import HookManager
from app.services.ingest import IngestService
from app.services.locks import NamespaceLockManager
from app.services.ntfy import NtfyPublisher
from app.services.settings_store import SettingsStore
from app.services.user_store import SESSION_COOKIE, UserStore
from app.storage.factory import build_storage_backend


def create_app(settings: Settings | None = None, user_store_path: Path | None = None) -> FastAPI:
    settings = settings or load_settings()
    if not settings.index_database_url and user_store_path is None:
        raise RuntimeError("Central requires PostgreSQL. Tests may pass user_store_path for an isolated SQLite app database.")
    logger = configure_logging(settings.log_level)
    storage_backend = build_storage_backend(settings)
    snapshot_index = build_snapshot_index_backend(settings)
    settings_store = SettingsStore(database_url=settings.index_database_url, sqlite_path=user_store_path)
    user_store = UserStore(database_url=settings.index_database_url, sqlite_path=user_store_path or app_database_path())
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
    app.state.user_store = user_store
    app.state.ingest_service = ingest_service
    app.state.hook_manager = hook_manager
    app.state.ntfy_publisher = ntfy_publisher
    app.state.cleanup_task = None
    app.include_router(admin_router)
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

    @app.middleware("http")
    async def require_web_session(request, call_next):
        path = request.url.path
        user = user_store.user_for_session(request.cookies.get(SESSION_COOKIE))
        request.state.current_user = user

        if _is_public_path(path):
            return await call_next(request)
        if path.startswith("/api/") and user is None:
            return JSONResponse(status_code=401, content={"detail": "login required"})
        if path.startswith("/api/") and user.get("must_change_password"):
            return JSONResponse(status_code=403, content={"detail": "password change required"})
        return await call_next(request)

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


def _is_public_path(path: str) -> bool:
    return (
        path == "/"
        or path.startswith("/static/")
        or path.startswith("/health")
        or path.startswith("/backup/uploads/")
        or path in {
            "/api/session/me",
            "/api/session/login",
            "/api/session/logout",
            "/api/session/change-password",
        }
    )
