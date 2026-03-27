from __future__ import annotations

from fastapi import FastAPI, File, Form, Header, HTTPException, UploadFile
from fastapi.responses import JSONResponse

from app.config import Settings, load_settings
from app.models import HealthResponse, UploadMetadata, UploadResponse
from app.services.ingest import IngestService
from app.services.locks import NamespaceLockManager
from app.storage.local import LocalFilesystemBackend
from app.utils.logging import configure_logging
from app.utils.paths import build_snapshot_filename, validate_namespace_component


def create_app() -> FastAPI:
    settings = load_settings()
    logger = configure_logging(settings.log_level)
    storage_backend = build_storage_backend(settings)
    lock_manager = NamespaceLockManager()
    ingest_service = IngestService(
        storage_backend=storage_backend,
        lock_manager=lock_manager,
        staging_dir=settings.staging_dir,
        max_upload_size_bytes=settings.max_upload_size_bytes,
        retention_keep_last=settings.retention_keep_last,
        logger=logger,
    )

    app = FastAPI(title="RelayCentralizer Central", version="0.1.0")
    app.state.settings = settings
    app.state.logger = logger
    app.state.storage_backend = storage_backend
    app.state.ingest_service = ingest_service

    @app.get("/health", response_model=HealthResponse)
    async def health() -> HealthResponse:
        if not storage_backend.healthcheck():
            raise HTTPException(status_code=503, detail="storage backend unavailable")
        return HealthResponse(status="ok")

    @app.post("/backup/upload", response_model=UploadResponse)
    async def upload_backup(
        edge_id: str = Form(...),
        job_name: str = Form(...),
        fingerprint: str = Form(...),
        timestamp: str = Form(...),
        archive_format: str = Form(...),
        archive: UploadFile = File(...),
        authorization: str | None = Header(default=None),
    ) -> UploadResponse:
        authorize_request(authorization, settings, logger)

        try:
            metadata = UploadMetadata(
                edge_id=validate_namespace_component(edge_id, "edge_id"),
                job_name=validate_namespace_component(job_name, "job_name"),
                fingerprint=fingerprint.strip(),
                timestamp=timestamp.strip(),
                archive_format=archive_format,
            )
        except ValueError as exc:
            logger.error("invalid_metadata detail=%s", exc)
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        except Exception as exc:
            logger.error("invalid_metadata detail=%s", exc)
            raise HTTPException(status_code=400, detail="invalid metadata") from exc

        stored_name = build_snapshot_filename(
            job_name=metadata.job_name,
            timestamp=metadata.timestamp,
            fingerprint=metadata.fingerprint,
        )
        namespace = f"{metadata.edge_id}/{metadata.job_name}"
        logger.info(
            "upload_received edge_id=%s job_name=%s filename=%s fingerprint=%s",
            metadata.edge_id,
            metadata.job_name,
            stored_name,
            metadata.fingerprint,
        )

        result = await ingest_service.ingest(
            namespace=namespace,
            filename=stored_name,
            metadata=metadata,
            archive=archive,
        )
        return UploadResponse(**result)

    @app.exception_handler(HTTPException)
    async def http_exception_handler(_, exc: HTTPException) -> JSONResponse:
        return JSONResponse(status_code=exc.status_code, content={"detail": exc.detail})

    return app


def build_storage_backend(settings: Settings) -> LocalFilesystemBackend:
    if settings.storage_backend != "local":
        raise RuntimeError(f"unsupported storage backend: {settings.storage_backend}")
    return LocalFilesystemBackend(settings.backup_root)


def authorize_request(authorization: str | None, settings: Settings, logger) -> None:
    expected = f"Bearer {settings.auth_token}"
    if authorization != expected:
        logger.warning("auth_failure")
        raise HTTPException(status_code=401, detail="unauthorized")


app = create_app()


if __name__ == "__main__":
    import uvicorn

    uvicorn.run("app.main:app", host="0.0.0.0", port=8000)
