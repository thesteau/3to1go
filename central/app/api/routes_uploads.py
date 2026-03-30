from __future__ import annotations

from fastapi import APIRouter, Depends, File, Form, Header, HTTPException, UploadFile

from app.api.auth import authorize_request
from app.api.dependencies import get_ingest_service, get_logger, get_settings
from app.api.models import UploadMetadata, UploadResponse
from app.core.config import Settings
from app.services.ingest import IngestService
from app.utils.paths import build_snapshot_filename, validate_namespace_component


router = APIRouter()


@router.post("/backup/upload", response_model=UploadResponse)
async def upload_backup(
    edge_id: str = Form(...),
    job_name: str = Form(...),
    fingerprint: str = Form(...),
    timestamp: str = Form(...),
    archive_format: str = Form(...),
    archive: UploadFile = File(...),
    authorization: str | None = Header(default=None),
    settings: Settings = Depends(get_settings),
    logger=Depends(get_logger),
    ingest_service: IngestService = Depends(get_ingest_service),
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
