from __future__ import annotations

import asyncio
import hashlib
import json
import os
import shutil
import tempfile
import uuid
from dataclasses import asdict, dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path

from fastapi import HTTPException

from app.api.models import UploadMetadata, UploadSessionResponse
from app.services.locks import NamespaceLockManager
from app.services.retention import prune_old_snapshots
from app.storage.base import StorageBackend


_TIME_FORMAT = "%Y-%m-%dT%H:%M:%SZ"


@dataclass(slots=True)
class UploadSession:
    upload_id: str
    idempotency_key: str
    namespace: str
    filename: str
    edge_id: str
    job_name: str
    fingerprint: str
    timestamp: str
    archive_format: str
    archive_size_bytes: int
    uploaded_bytes: int
    status: str
    created_at: str
    updated_at: str
    expires_at: str
    stored_as: str | None = None
    pruned: int = 0


class IngestService:
    def __init__(
        self,
        storage_backend: StorageBackend,
        lock_manager: NamespaceLockManager,
        staging_dir: Path,
        max_upload_size_bytes: int,
        recommended_chunk_size_bytes: int,
        upload_session_ttl_hours: int,
        retention_keep_last: int,
        logger,
    ) -> None:
        self.storage_backend = storage_backend
        self.lock_manager = lock_manager
        self.staging_dir = staging_dir
        self.max_upload_size_bytes = max_upload_size_bytes
        self.recommended_chunk_size_bytes = recommended_chunk_size_bytes
        self.upload_session_ttl = timedelta(hours=upload_session_ttl_hours)
        self.retention_keep_last = retention_keep_last
        self.logger = logger
        self.staging_dir.mkdir(parents=True, exist_ok=True)
        self.upload_root = self.staging_dir / "uploads"
        self.key_root = self.upload_root / "keys"
        self.upload_root.mkdir(parents=True, exist_ok=True)
        self.key_root.mkdir(parents=True, exist_ok=True)
        self._session_locks: dict[str, asyncio.Lock] = {}
        self._session_manager_lock = asyncio.Lock()

    async def start_upload(
        self,
        namespace: str,
        filename: str,
        metadata: UploadMetadata,
        archive_size_bytes: int,
        idempotency_key: str,
    ) -> UploadSessionResponse:
        self.cleanup_stale_uploads()

        existing = self._load_session_for_key(idempotency_key)
        if existing is not None:
            existing.uploaded_bytes = self._current_upload_size(existing.upload_id)
            existing.updated_at = _utc_now_text()
            existing.expires_at = _utc_text_after(self.upload_session_ttl)
            self._save_session(existing)
            return self._build_session_response(existing)

        self._validate_capacity(archive_size_bytes)

        created_at = _utc_now_text()
        session = UploadSession(
            upload_id=uuid.uuid4().hex,
            idempotency_key=idempotency_key,
            namespace=namespace,
            filename=filename,
            edge_id=metadata.edge_id,
            job_name=metadata.job_name,
            fingerprint=metadata.fingerprint,
            timestamp=metadata.timestamp,
            archive_format=metadata.archive_format,
            archive_size_bytes=archive_size_bytes,
            uploaded_bytes=0,
            status="initiated",
            created_at=created_at,
            updated_at=created_at,
            expires_at=_utc_text_after(self.upload_session_ttl),
        )
        self._upload_data_path(session.upload_id).parent.mkdir(parents=True, exist_ok=True)
        self._save_session(session)
        self._write_key_mapping(idempotency_key, session.upload_id)
        return self._build_session_response(session)

    async def append_chunk(self, upload_id: str, offset: int, chunk_stream) -> dict:
        lock = await self._get_session_lock(upload_id)
        async with lock:
            session = self._load_session(upload_id)
            current_size = self._current_upload_size(upload_id)

            if session.status == "completed":
                return {
                    "upload_id": upload_id,
                    "status": session.status,
                    "next_offset": session.archive_size_bytes,
                    "received_bytes": 0,
                }

            if offset != current_size:
                raise HTTPException(
                    status_code=409,
                    detail={
                        "status": "offset_mismatch",
                        "next_offset": current_size,
                        "upload_id": upload_id,
                    },
                )

            stage_path = self._upload_data_path(upload_id)
            stage_path.parent.mkdir(parents=True, exist_ok=True)
            staging_free = self._free_space_bytes(self.staging_dir)
            bytes_received = 0

            try:
                with stage_path.open("ab") as handle:
                    async for chunk in chunk_stream:
                        if not chunk:
                            continue
                        bytes_received += len(chunk)
                        if current_size + bytes_received > session.archive_size_bytes:
                            raise HTTPException(status_code=400, detail="chunk exceeds declared upload size")
                        if bytes_received > staging_free:
                            raise HTTPException(status_code=507, detail="insufficient staging storage")
                        handle.write(chunk)
                    handle.flush()
                    os.fsync(handle.fileno())
            except HTTPException:
                raise
            except OSError as exc:
                self.logger.error("chunk_write_failure upload_id=%s detail=%s", upload_id, exc)
                raise HTTPException(status_code=500, detail="failed to persist upload chunk") from exc

            session.uploaded_bytes = current_size + bytes_received
            session.status = "uploaded" if session.uploaded_bytes >= session.archive_size_bytes else "in_progress"
            session.updated_at = _utc_now_text()
            session.expires_at = _utc_text_after(self.upload_session_ttl)
            self._save_session(session)
            return {
                "upload_id": upload_id,
                "status": session.status,
                "next_offset": session.uploaded_bytes,
                "received_bytes": bytes_received,
            }

    async def finalize_upload(self, upload_id: str) -> dict:
        session = self._load_session(upload_id)
        if session.status == "completed":
            return {
                "status": "ok",
                "stored_as": session.stored_as or session.filename,
                "pruned": session.pruned,
            }

        lock = await self.lock_manager.get_lock(session.namespace)
        async with lock:
            session = self._load_session(upload_id)
            current_size = self._current_upload_size(upload_id)
            if current_size != session.archive_size_bytes:
                raise HTTPException(
                    status_code=409,
                    detail={
                        "status": "incomplete_upload",
                        "next_offset": current_size,
                        "upload_id": upload_id,
                    },
                )

            staged_path = self._upload_data_path(upload_id)
            try:
                storage_result = self.storage_backend.store(session.namespace, session.filename, staged_path)
                pruned = prune_old_snapshots(
                    self.storage_backend,
                    namespace=session.namespace,
                    keep_last=self.retention_keep_last,
                )
            except Exception:
                self.logger.exception(
                    "unexpected_exception edge_id=%s job_name=%s upload_id=%s",
                    session.edge_id,
                    session.job_name,
                    session.upload_id,
                )
                raise

            session.uploaded_bytes = session.archive_size_bytes
            session.status = "completed"
            session.stored_as = storage_result["stored_as"]
            session.pruned = pruned
            session.updated_at = _utc_now_text()
            session.expires_at = _utc_text_after(self.upload_session_ttl)
            self._save_session(session)
            self.logger.info(
                "commit_success edge_id=%s job_name=%s upload_id=%s stored_as=%s",
                session.edge_id,
                session.job_name,
                session.upload_id,
                storage_result["stored_as"],
            )
            self.logger.info(
                "prune_result edge_id=%s job_name=%s upload_id=%s pruned=%s",
                session.edge_id,
                session.job_name,
                session.upload_id,
                pruned,
            )
            return {"status": "ok", "stored_as": storage_result["stored_as"], "pruned": pruned}

    def cleanup_stale_uploads(self) -> None:
        cutoff = datetime.now(timezone.utc)
        for metadata_path in self.upload_root.glob("*/metadata.json"):
            try:
                session = self._session_from_json(json.loads(metadata_path.read_text(encoding="utf-8")))
                expires_at = datetime.strptime(session.expires_at, _TIME_FORMAT).replace(tzinfo=timezone.utc)
            except (OSError, ValueError, json.JSONDecodeError):
                continue

            if expires_at > cutoff:
                continue

            key_path = self._key_mapping_path(session.idempotency_key)
            key_path.unlink(missing_ok=True)
            shutil.rmtree(metadata_path.parent, ignore_errors=True)

    def _validate_capacity(self, archive_size_bytes: int) -> None:
        if archive_size_bytes > self.max_upload_size_bytes:
            raise HTTPException(status_code=413, detail="upload too large")

        staging_free = self._free_space_bytes(self.staging_dir)
        backup_root = getattr(self.storage_backend, "backup_root", None)
        backup_free = self._free_space_bytes(backup_root) if backup_root is not None else None
        if archive_size_bytes > staging_free:
            raise HTTPException(status_code=507, detail="insufficient staging storage")
        if backup_free is not None and archive_size_bytes > backup_free:
            raise HTTPException(status_code=507, detail="insufficient backup storage")

    def _free_space_bytes(self, path: Path) -> int:
        path.mkdir(parents=True, exist_ok=True)
        return shutil.disk_usage(path).free

    async def _get_session_lock(self, upload_id: str) -> asyncio.Lock:
        async with self._session_manager_lock:
            lock = self._session_locks.get(upload_id)
            if lock is None:
                lock = asyncio.Lock()
                self._session_locks[upload_id] = lock
            return lock

    def _build_session_response(self, session: UploadSession) -> UploadSessionResponse:
        return UploadSessionResponse(
            upload_id=session.upload_id,
            status=session.status,
            next_offset=session.archive_size_bytes if session.status == "completed" else session.uploaded_bytes,
            archive_size_bytes=session.archive_size_bytes,
            recommended_chunk_size_bytes=min(self.recommended_chunk_size_bytes, session.archive_size_bytes),
            stored_as=session.stored_as,
            pruned=session.pruned,
        )

    def _session_dir(self, upload_id: str) -> Path:
        return self.upload_root / upload_id

    def _metadata_path(self, upload_id: str) -> Path:
        return self._session_dir(upload_id) / "metadata.json"

    def _upload_data_path(self, upload_id: str) -> Path:
        return self._session_dir(upload_id) / "archive.part"

    def _key_mapping_path(self, idempotency_key: str) -> Path:
        digest = hashlib.sha256(idempotency_key.encode("utf-8")).hexdigest()
        return self.key_root / f"{digest}.json"

    def _write_key_mapping(self, idempotency_key: str, upload_id: str) -> None:
        path = self._key_mapping_path(idempotency_key)
        payload = {"idempotency_key": idempotency_key, "upload_id": upload_id}
        with tempfile.NamedTemporaryFile(
            "w",
            encoding="utf-8",
            dir=self.key_root,
            delete=False,
            suffix=".tmp",
        ) as handle:
            json.dump(payload, handle, indent=2, sort_keys=True)
            handle.flush()
            temp_path = Path(handle.name)
        temp_path.replace(path)

    def _load_session_for_key(self, idempotency_key: str) -> UploadSession | None:
        path = self._key_mapping_path(idempotency_key)
        if not path.exists():
            return None
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return None
        upload_id = str(payload.get("upload_id") or "").strip()
        if not upload_id:
            return None
        try:
            return self._load_session(upload_id)
        except HTTPException:
            path.unlink(missing_ok=True)
            return None

    def _load_session(self, upload_id: str) -> UploadSession:
        metadata_path = self._metadata_path(upload_id)
        if not metadata_path.exists():
            raise HTTPException(status_code=404, detail="upload session not found")
        try:
            payload = json.loads(metadata_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            raise HTTPException(status_code=500, detail="failed to load upload session") from exc
        return self._session_from_json(payload)

    def _save_session(self, session: UploadSession) -> None:
        session_dir = self._session_dir(session.upload_id)
        session_dir.mkdir(parents=True, exist_ok=True)
        metadata_path = self._metadata_path(session.upload_id)
        with tempfile.NamedTemporaryFile(
            "w",
            encoding="utf-8",
            dir=session_dir,
            delete=False,
            suffix=".tmp",
        ) as handle:
            json.dump(asdict(session), handle, indent=2, sort_keys=True)
            handle.flush()
            temp_path = Path(handle.name)
        temp_path.replace(metadata_path)

    def _session_from_json(self, payload: dict) -> UploadSession:
        return UploadSession(**payload)

    def _current_upload_size(self, upload_id: str) -> int:
        data_path = self._upload_data_path(upload_id)
        if not data_path.exists():
            return 0
        return data_path.stat().st_size


def _utc_now_text() -> str:
    return datetime.now(timezone.utc).strftime(_TIME_FORMAT)


def _utc_text_after(delta: timedelta) -> str:
    return (datetime.now(timezone.utc) + delta).strftime(_TIME_FORMAT)
