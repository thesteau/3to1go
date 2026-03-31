from __future__ import annotations

import os
import shutil
import uuid
from pathlib import Path

from fastapi import HTTPException, UploadFile

from app.api.models import UploadMetadata
from app.services.locks import NamespaceLockManager
from app.services.retention import prune_old_snapshots
from app.storage.base import StorageBackend


class IngestService:
    def __init__(
        self,
        storage_backend: StorageBackend,
        lock_manager: NamespaceLockManager,
        staging_dir: Path,
        max_upload_size_bytes: int,
        retention_keep_last: int,
        logger,
    ) -> None:
        self.storage_backend = storage_backend
        self.lock_manager = lock_manager
        self.staging_dir = staging_dir
        self.max_upload_size_bytes = max_upload_size_bytes
        self.retention_keep_last = retention_keep_last
        self.logger = logger
        self.staging_dir.mkdir(parents=True, exist_ok=True)

    async def ingest(
        self,
        namespace: str,
        filename: str,
        metadata: UploadMetadata,
        archive: UploadFile,
    ) -> dict:
        staged_path: Path | None = None
        lock = await self.lock_manager.get_lock(namespace)

        async with lock:
            try:
                staged_path = await self._stage_upload(archive)
                storage_result = self.storage_backend.store(namespace, filename, staged_path)
                pruned = prune_old_snapshots(self.storage_backend, namespace=namespace, keep_last=self.retention_keep_last)
                self.logger.info("commit_success edge_id=%s job_name=%s stored_as=%s", metadata.edge_id, metadata.job_name, storage_result["stored_as"])
                self.logger.info("prune_result edge_id=%s job_name=%s pruned=%s", metadata.edge_id, metadata.job_name, pruned)
                return {"status": "ok", "stored_as": storage_result["stored_as"], "pruned": pruned}
            except HTTPException:
                raise
            except Exception:
                self.logger.exception("unexpected_exception edge_id=%s job_name=%s", metadata.edge_id, metadata.job_name)
                raise
            finally:
                await archive.close()
                if staged_path is not None and staged_path.exists():
                    staged_path.unlink(missing_ok=True)

    def _free_space_bytes(self, path: Path) -> int:
        path.mkdir(parents=True, exist_ok=True)
        return shutil.disk_usage(path).free

    async def _stage_upload(self, archive: UploadFile) -> Path:
        staged_path = self.staging_dir / f"{uuid.uuid4().hex}.part"
        bytes_written = 0

        staging_free = self._free_space_bytes(self.staging_dir)
        backup_root = getattr(self.storage_backend, "backup_root", None)
        backup_free = self._free_space_bytes(backup_root) if backup_root is not None else None

        try:
            with staged_path.open("wb") as handle:
                while True:
                    chunk = await archive.read(1024 * 1024)
                    if not chunk:
                        break
                    bytes_written += len(chunk)
                    if bytes_written > self.max_upload_size_bytes:
                        raise HTTPException(status_code=413, detail="upload too large")
                    if bytes_written > staging_free:
                        raise HTTPException(status_code=507, detail="insufficient staging storage")
                    if backup_free is not None and bytes_written > backup_free:
                        raise HTTPException(status_code=507, detail="insufficient backup storage")
                    handle.write(chunk)
                handle.flush()
                os.fsync(handle.fileno())
        except HTTPException:
            staged_path.unlink(missing_ok=True)
            raise
        except OSError as exc:
            staged_path.unlink(missing_ok=True)
            self.logger.error("temp_write_failure detail=%s", exc)
            raise HTTPException(status_code=500, detail="failed to stage upload") from exc

        return staged_path
