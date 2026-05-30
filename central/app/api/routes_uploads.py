from __future__ import annotations

from fastapi import APIRouter, Body, Depends, Header, HTTPException, Query, Request

from app.api.auth import authorize_request
from app.api.dependencies import get_ingest_service, get_logger, get_settings
from app.api.models import (
    UploadChunkResponse,
    UploadInitRequest,
    UploadMetadata,
    UploadResponse,
    UploadSessionResponse,
)
from app.core.config import Settings
from app.services.ingest import IngestService
from app.utils.paths import build_snapshot_filename, validate_namespace_component


router = APIRouter()


def _forwarded_source_address(request: Request) -> str | None:
    x_forwarded_for = request.headers.get("x-forwarded-for")
    if x_forwarded_for:
        candidate = x_forwarded_for.split(",", 1)[0].strip()
        return candidate or None

    forwarded = request.headers.get("forwarded")
    if not forwarded:
        return None

    first_hop = forwarded.split(",", 1)[0]
    for item in first_hop.split(";"):
        entry = item.strip()
        if not entry.lower().startswith("for="):
            continue
        candidate = entry[4:].strip().strip('"')
        if candidate.startswith("[") and candidate.endswith("]"):
            candidate = candidate[1:-1]
        if candidate.startswith("_"):
            return None
        return candidate or None
    return None


def _request_source_address(request: Request) -> str | None:
    forwarded = _forwarded_source_address(request)
    if forwarded:
        return forwarded

    client = request.client
    if client is None:
        return None
    return client.host or None


@router.post("/backup/uploads/initiate", response_model=UploadSessionResponse)
async def initiate_upload(
    request: Request,
    payload: UploadInitRequest,
    authorization: str | None = Header(default=None),
    settings: Settings = Depends(get_settings),
    logger=Depends(get_logger),
    ingest_service: IngestService = Depends(get_ingest_service),
) -> UploadSessionResponse:
    authorize_request(authorization, settings, logger)

    try:
        metadata = UploadMetadata(
            edge_id=validate_namespace_component(payload.edge_id, "edge_id"),
            edge_instance_id=payload.edge_instance_id,
            job_name=validate_namespace_component(payload.job_name, "job_name"),
            fingerprint=payload.fingerprint.strip(),
            timestamp=payload.timestamp.strip(),
            archive_format=payload.archive_format,
            encryption_key_fingerprint=payload.encryption_key_fingerprint,
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
        "upload_session_requested edge_id=%s job_name=%s filename=%s fingerprint=%s size=%s",
        metadata.edge_id,
        metadata.job_name,
        stored_name,
        metadata.fingerprint,
        payload.archive_size_bytes,
    )

    result = await ingest_service.start_upload(
        namespace=namespace,
        filename=stored_name,
        metadata=metadata,
        archive_size_bytes=payload.archive_size_bytes,
        archive_sha256=payload.archive_sha256,
        idempotency_key=payload.idempotency_key.strip(),
        source_address=_request_source_address(request),
    )
    return result


@router.put("/backup/uploads/{upload_id}/chunk", response_model=UploadChunkResponse)
async def append_upload_chunk(
    upload_id: str,
    request: Request,
    offset: int = Query(..., ge=0),
    authorization: str | None = Header(default=None),
    settings: Settings = Depends(get_settings),
    logger=Depends(get_logger),
    ingest_service: IngestService = Depends(get_ingest_service),
) -> UploadChunkResponse:
    authorize_request(authorization, settings, logger)
    result = await ingest_service.append_chunk(upload_id, offset, request.stream())
    return UploadChunkResponse(**result)


@router.post("/backup/uploads/{upload_id}/finalize", response_model=UploadResponse)
async def finalize_upload(
    upload_id: str,
    _body: bytes = Body(default=b""),
    authorization: str | None = Header(default=None),
    settings: Settings = Depends(get_settings),
    logger=Depends(get_logger),
    ingest_service: IngestService = Depends(get_ingest_service),
) -> UploadResponse:
    authorize_request(authorization, settings, logger)
    result = await ingest_service.finalize_upload(upload_id)
    return UploadResponse(**result)
