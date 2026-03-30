from __future__ import annotations

from fastapi import FastAPI, HTTPException
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles

from app.api.routes_overview import router as overview_router
from app.api.routes_uploads import router as uploads_router
from app.api.views import STATIC_DIR
from app.core.config import Settings, load_settings
from app.core.logging import configure_logging
from app.services.ingest import IngestService
from app.services.locks import NamespaceLockManager
from app.storage.factory import build_storage_backend


def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or load_settings()
    logger = configure_logging(settings.log_level)
    storage_backend = build_storage_backend(settings)
    ingest_service = IngestService(
        storage_backend=storage_backend,
        lock_manager=NamespaceLockManager(),
        staging_dir=settings.staging_dir,
        max_upload_size_bytes=settings.max_upload_size_bytes,
        retention_keep_last=settings.retention_keep_last,
        logger=logger,
    )

    app = FastAPI(title="RelayCentralizer Central", version="0.1.0")
    app.mount("/static", StaticFiles(directory=str(STATIC_DIR)), name="static")
    app.state.settings = settings
    app.state.logger = logger
    app.state.storage_backend = storage_backend
    app.state.ingest_service = ingest_service
    app.include_router(overview_router)
    app.include_router(uploads_router)

    @app.exception_handler(HTTPException)
    async def http_exception_handler(_, exc: HTTPException) -> JSONResponse:
        return JSONResponse(status_code=exc.status_code, content={"detail": exc.detail})

    return app
