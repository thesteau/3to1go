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
from app.index.base import SnapshotIndexBackend
from app.services.locks import NamespaceLockManager
from app.services.retention import prune_old_snapshots
from app.storage.base import StorageBackend


_TIME_FORMAT = "%Y-%m-%dT%H:%M:%SZ"
_ACTIVE_SESSION_STATUSES = {"initiated", "in_progress", "uploaded", "checksum_retry_required"}


@dataclass(slots=True)
class UploadSession:
    upload_id: str
    idempotency_key: str
    namespace: str
    filename: str
    edge_id: str
    edge_instance_id: str
    job_name: str
    fingerprint: str
    timestamp: str
    archive_format: str
    archive_size_bytes: int
    archive_sha256: str
    source_address: str | None
    uploaded_bytes: int
    status: str
    created_at: str
    updated_at: str
    expires_at: str
    stored_as: str | None = None
    pruned: int = 0


@dataclass(slots=True)
class EdgeRegistration:
    edge_id: str
    edge_instance_id: str
    encryption_key_fingerprint: str | None
    advertised_url: str | None
    first_seen_at: str
    last_seen_at: str
    credential_hash: str | None = None


class IngestService:
    def __init__(
        self,
        settings,
        storage_backend: StorageBackend,
        snapshot_index: SnapshotIndexBackend,
        lock_manager: NamespaceLockManager,
        staging_dir: Path,
        max_upload_size_bytes: int,
        recommended_chunk_size_bytes: int,
        upload_session_ttl_hours: int,
        retention_keep_last: int,
        logger,
        hook_manager,
        ntfy_publisher,
    ) -> None:
        self.settings = settings
        self.storage_backend = storage_backend
        self.snapshot_index = snapshot_index
        self.lock_manager = lock_manager
        self.staging_dir = staging_dir
        self.max_upload_size_bytes = max_upload_size_bytes
        self.recommended_chunk_size_bytes = recommended_chunk_size_bytes
        self.upload_session_ttl = timedelta(hours=upload_session_ttl_hours)
        self.retention_keep_last = retention_keep_last
        self.logger = logger
        self.hook_manager = hook_manager
        self.ntfy_publisher = ntfy_publisher
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
        archive_sha256: str,
        idempotency_key: str,
        source_address: str | None = None,
        credential_hash: str | None = None,
    ) -> UploadSessionResponse:
        self.cleanup_stale_uploads()
        registration_lock = await self.lock_manager.get_lock(f"edge-registry:{metadata.edge_id}")
        async with registration_lock:
            self._register_edge(metadata, credential_hash)

        existing = self._load_session_for_key(idempotency_key)
        if existing is not None:
            if self._session_references_missing_snapshot(existing):
                self.reconcile_namespace(existing.namespace)
                self._discard_session(existing)
                existing = None
        if existing is not None:
            if existing.archive_sha256 != archive_sha256:
                raise HTTPException(status_code=409, detail="idempotency key reused with different archive checksum")
            existing.uploaded_bytes = self._current_upload_size(existing.upload_id)
            existing.updated_at = _utc_now_text()
            existing.expires_at = _utc_text_after(self.upload_session_ttl)
            self._save_session(existing)
            return self._build_session_response(existing)

        committed_duplicate = self.snapshot_index.find_duplicate(namespace, archive_sha256)
        if committed_duplicate is not None:
            return self._build_committed_duplicate_response(
                archive_size_bytes=archive_size_bytes,
                stored_as=committed_duplicate["stored_as"],
            )

        self._validate_new_reservation(archive_size_bytes)

        created_at = _utc_now_text()
        session = UploadSession(
            upload_id=uuid.uuid4().hex,
            idempotency_key=idempotency_key,
            namespace=namespace,
            filename=filename,
            edge_id=metadata.edge_id,
            edge_instance_id=metadata.edge_instance_id or "",
            job_name=metadata.job_name,
            fingerprint=metadata.fingerprint,
            timestamp=metadata.timestamp,
            archive_format=metadata.archive_format,
            archive_size_bytes=archive_size_bytes,
            archive_sha256=archive_sha256,
            source_address=source_address,
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
            bytes_received = 0

            try:
                with stage_path.open("ab") as handle:
                    async for chunk in chunk_stream:
                        if not chunk:
                            continue
                        bytes_received += len(chunk)
                        if current_size + bytes_received > session.archive_size_bytes:
                            raise HTTPException(status_code=400, detail="chunk exceeds declared upload size")
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
            if self._session_references_missing_snapshot(session):
                self.reconcile_namespace(session.namespace)
                self._discard_session(session)
                raise HTTPException(status_code=409, detail="stored snapshot missing; re-initiate upload")
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
            actual_sha256 = _sha256_path(staged_path)
            if actual_sha256 != session.archive_sha256:
                self.logger.error(
                    "checksum_mismatch upload_id=%s expected=%s actual=%s",
                    upload_id,
                    session.archive_sha256,
                    actual_sha256,
                )
                staged_path.unlink(missing_ok=True)
                session.uploaded_bytes = 0
                session.status = "checksum_retry_required"
                session.updated_at = _utc_now_text()
                session.expires_at = _utc_text_after(self.upload_session_ttl)
                self._save_session(session)
                raise HTTPException(
                    status_code=409,
                    detail={
                        "status": "checksum_mismatch",
                        "next_offset": 0,
                        "upload_id": upload_id,
                    },
                )

            hook_context = self._hook_context(session, staged_path=staged_path)
            self.hook_manager.run_command(self.settings.hook_pre_command, phase="pre", context=hook_context)
            hook_status = "error"
            hook_stored_as = session.filename
            hook_pruned = 0
            hook_duplicate = False

            committed_duplicate = self.snapshot_index.find_duplicate(session.namespace, actual_sha256)
            try:
                if committed_duplicate is not None:
                    staged_path.unlink(missing_ok=True)
                    session.uploaded_bytes = session.archive_size_bytes
                    session.status = "completed"
                    session.stored_as = committed_duplicate["stored_as"]
                    session.pruned = 0
                    session.updated_at = _utc_now_text()
                    session.expires_at = _utc_text_after(self.upload_session_ttl)
                    self._save_session(session)
                    self.logger.info(
                        "duplicate_upload_rejected edge_id=%s job_name=%s upload_id=%s stored_as=%s",
                        session.edge_id,
                        session.job_name,
                        session.upload_id,
                        committed_duplicate["stored_as"],
                    )
                    hook_status = "ok"
                    hook_stored_as = committed_duplicate["stored_as"]
                    hook_duplicate = True
                    result = {
                        "status": "ok",
                        "stored_as": committed_duplicate["stored_as"],
                        "pruned": 0,
                        "duplicate": True,
                    }
                else:
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

                    current_snapshots = self.storage_backend.list(session.namespace)
                    current_snapshot = next(
                        (item for item in current_snapshots if item.get("filename") == storage_result["stored_as"]),
                        None,
                    )
                    self.snapshot_index.upsert_snapshot(
                        session.namespace,
                        {
                            "stored_as": storage_result["stored_as"],
                            "archive_sha256": actual_sha256,
                            "fingerprint": session.fingerprint,
                            "timestamp": session.timestamp,
                            "size_bytes": (current_snapshot or {}).get("size_bytes", 0),
                            "mtime": (current_snapshot or {}).get("mtime", 0),
                        },
                    )
                    self.snapshot_index.reconcile_namespace(session.namespace, current_snapshots)
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
                    hook_status = "ok"
                    hook_stored_as = storage_result["stored_as"]
                    hook_pruned = pruned
                    result = {
                        "status": "ok",
                        "stored_as": storage_result["stored_as"],
                        "pruned": pruned,
                        "duplicate": False,
                    }
            finally:
                final_hook_context = {
                    **hook_context,
                    "status": hook_status,
                    "stored_as": hook_stored_as,
                    "pruned": hook_pruned,
                    "duplicate": hook_duplicate,
                }
                self.hook_manager.run_command(self.settings.hook_post_command, phase="post", context=final_hook_context)
                if hook_status == "ok":
                    self.ntfy_publisher.publish_best_effort(self.settings, final_hook_context)

            return result

    async def cleanup_loop(self, interval_seconds: int) -> None:
        while True:
            try:
                self.cleanup_stale_uploads()
            except Exception:
                self.logger.exception("upload_cleanup_failed")
            await asyncio.sleep(interval_seconds)

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

    def _validate_new_reservation(self, archive_size_bytes: int) -> None:
        if archive_size_bytes > self.max_upload_size_bytes:
            raise HTTPException(status_code=413, detail="upload too large")

        reserved_bytes = self._reserved_bytes()
        staging_free = self._free_space_bytes(self.staging_dir)
        backup_root = getattr(self.storage_backend, "backup_root", None)
        backup_free = self._free_space_bytes(backup_root) if backup_root is not None else None

        if archive_size_bytes + reserved_bytes > staging_free:
            raise HTTPException(status_code=507, detail="insufficient staging storage")
        if backup_free is not None and archive_size_bytes + reserved_bytes > backup_free:
            raise HTTPException(status_code=507, detail="insufficient backup storage")

    def _reserved_bytes(self) -> int:
        total = 0
        for metadata_path in self.upload_root.glob("*/metadata.json"):
            try:
                session = self._session_from_json(json.loads(metadata_path.read_text(encoding="utf-8")))
            except (OSError, TypeError, ValueError, json.JSONDecodeError):
                continue
            if session.status in _ACTIVE_SESSION_STATUSES:
                total += session.archive_size_bytes
        return total

    def _free_space_bytes(self, path: Path | None) -> int:
        if path is None:
            return 0
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
            duplicate=False,
        )

    def _session_references_missing_snapshot(self, session: UploadSession) -> bool:
        if session.status != "completed":
            return False
        stored_name = session.stored_as or session.filename
        return not self._snapshot_exists(session.namespace, stored_name)

    def _snapshot_exists(self, namespace: str, filename: str) -> bool:
        return any(item.get("filename") == filename for item in self.storage_backend.list(namespace))

    def _discard_session(self, session: UploadSession) -> None:
        self._key_mapping_path(session.idempotency_key).unlink(missing_ok=True)
        shutil.rmtree(self._session_dir(session.upload_id), ignore_errors=True)

    def list_edge_registrations(self, edge_id: str | None = None) -> list[dict]:
        return self.snapshot_index.list_edge_registrations(edge_id)

    def reconcile_namespace(self, namespace: str) -> None:
        self.snapshot_index.reconcile_namespace(namespace, self.storage_backend.list(namespace))

    def _build_committed_duplicate_response(self, *, archive_size_bytes: int, stored_as: str) -> UploadSessionResponse:
        return UploadSessionResponse(
            upload_id=f"committed-{stored_as}",
            status="completed",
            next_offset=archive_size_bytes,
            archive_size_bytes=archive_size_bytes,
            recommended_chunk_size_bytes=min(self.recommended_chunk_size_bytes, archive_size_bytes),
            stored_as=stored_as,
            pruned=0,
            duplicate=True,
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
        normalized = dict(payload)
        normalized.setdefault("edge_instance_id", "")
        normalized.setdefault("source_address", None)
        return UploadSession(**normalized)

    def _register_edge(self, metadata: UploadMetadata, credential_hash: str | None) -> None:
        edge_instance_id = (metadata.edge_instance_id or "").strip()
        if not edge_instance_id:
            return

        now = _utc_now_text()
        existing_payload = self.snapshot_index.get_edge_registration(metadata.edge_id, edge_instance_id)
        if existing_payload:
            normalized_existing = dict(existing_payload)
            normalized_existing.setdefault("advertised_url", None)
            normalized_existing.setdefault("credential_hash", None)
            existing = EdgeRegistration(**normalized_existing)
        else:
            existing = None

        registration = existing or EdgeRegistration(
            edge_id=metadata.edge_id,
            edge_instance_id=edge_instance_id,
            encryption_key_fingerprint=metadata.encryption_key_fingerprint,
            advertised_url=metadata.advertised_url,
            first_seen_at=now,
            last_seen_at=now,
        )
        registration.last_seen_at = now
        if metadata.encryption_key_fingerprint:
            registration.encryption_key_fingerprint = metadata.encryption_key_fingerprint
        if metadata.advertised_url is not None:
            registration.advertised_url = metadata.advertised_url
        if credential_hash:
            registration.credential_hash = credential_hash
        self.snapshot_index.upsert_edge_registration(asdict(registration))

    def _current_upload_size(self, upload_id: str) -> int:
        data_path = self._upload_data_path(upload_id)
        if not data_path.exists():
            return 0
        return data_path.stat().st_size

    def _hook_context(self, session: UploadSession, *, staged_path: Path) -> dict[str, str | int | bool | None]:
        return {
            "edge_id": session.edge_id,
            "edge_instance_id": session.edge_instance_id,
            "job_name": session.job_name,
            "upload_id": session.upload_id,
            "namespace": session.namespace,
            "filename": session.filename,
            "fingerprint": session.fingerprint,
            "timestamp": session.timestamp,
            "archive_sha256": session.archive_sha256,
            "archive_size_bytes": session.archive_size_bytes,
            "source_address": session.source_address,
            "staged_path": str(staged_path),
        }


def _sha256_path(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while True:
            chunk = handle.read(1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
    return digest.hexdigest()


def _utc_now_text() -> str:
    return datetime.now(timezone.utc).strftime(_TIME_FORMAT)


def _utc_text_after(delta: timedelta) -> str:
    return (datetime.now(timezone.utc) + delta).strftime(_TIME_FORMAT)
