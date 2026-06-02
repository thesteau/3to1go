from __future__ import annotations

import tempfile
from dataclasses import dataclass
from pathlib import Path

from app.backup.archiver import extract_archive
from app.backup.discovery import JobDefinition
from app.core.config import Settings, encryption_key_path
from app.core.encryption import decrypt_file, load_or_create_key
from app.services.state import StateStore
from app.services.upload import UploadClient, UploadFailure


@dataclass(slots=True)
class RecoveryError(RuntimeError):
    message: str
    status_code: int = 500

    def __str__(self) -> str:
        return self.message


class RecoveryService:
    def __init__(
        self,
        settings: Settings,
        logger,
        state_store: StateStore,
        upload_client: UploadClient,
    ) -> None:
        self.settings = settings
        self.logger = logger
        self.state_store = state_store
        self.upload_client = upload_client

    def recover_latest(self, job: JobDefinition) -> dict[str, object]:
        state = self._begin_recovery(job)
        download_path = self._temp_path(".download.tar.zst")
        decrypted_path = self._temp_path(".decrypted.tar.zst")
        try:
            download = self.upload_client.download_latest_snapshot(
                edge_id=self.settings.edge_id,
                job_name=job.job_name,
                destination=download_path,
            )
            return self._finalize_recovery(job, state, download, download_path, decrypted_path)
        except (UploadFailure, RecoveryError, Exception) as error:  # noqa: BLE001
            self._handle_recovery_error(job, state, error)
        finally:
            download_path.unlink(missing_ok=True)
            decrypted_path.unlink(missing_ok=True)

    def recover_by_fingerprint(self, job: JobDefinition, fingerprint: str) -> dict[str, object]:
        state = self._begin_recovery(job)
        download_path = self._temp_path(".download.tar.zst")
        decrypted_path = self._temp_path(".decrypted.tar.zst")
        try:
            download = self.upload_client.download_snapshot_by_fingerprint(
                edge_id=self.settings.edge_id,
                job_name=job.job_name,
                fingerprint=fingerprint,
                destination=download_path,
            )
            return self._finalize_recovery(job, state, download, download_path, decrypted_path)
        except (UploadFailure, RecoveryError, Exception) as error:  # noqa: BLE001
            self._handle_recovery_error(job, state, error)
        finally:
            download_path.unlink(missing_ok=True)
            decrypted_path.unlink(missing_ok=True)

    def _begin_recovery(self, job: JobDefinition):
        state = self.state_store.get(job.state_key)
        state.job_name = job.job_name
        state.last_status = "recovering"
        state.last_error_category = None
        state.last_error_detail = None
        self.state_store.set(job.state_key, state)
        return state

    def _finalize_recovery(self, job, state, download, download_path, decrypted_path):
        snapshot_filename = str(download["filename"])
        key = load_or_create_key(encryption_key_path())
        try:
            decrypt_file(key, download_path, decrypted_path)
        except Exception as exc:
            raise RecoveryError(
                "unable to decrypt snapshot with this Edge key", status_code=409
            ) from exc

        restored_files = extract_archive(decrypted_path, job.root_path)

        state.last_status = "recovered"
        state.last_error_category = None
        state.last_error_detail = None
        self.state_store.set(job.state_key, state)
        self.logger.info(
            "recovery_success job_name=%s path=%s snapshot=%s restored_files=%s",
            job.job_name,
            job.root_path,
            snapshot_filename,
            restored_files,
        )
        return {
            "status": "recovered",
            "job_name": job.job_name,
            "snapshot_filename": snapshot_filename,
            "restored_files": restored_files,
        }

    def _handle_recovery_error(self, job, state, error: BaseException) -> None:
        if isinstance(error, UploadFailure):
            if error.status_code == 404:
                wrapped = RecoveryError("no snapshots found on Central", status_code=404)
            elif error.category == "unauthorized":
                wrapped = RecoveryError(
                    "Central rejected the recovery request; check the shared auth token",
                    status_code=502,
                )
            elif error.category in {"network", "server", "rate_limited", "circuit_open"}:
                wrapped = RecoveryError(str(error), status_code=502)
            else:
                wrapped = RecoveryError(str(error), status_code=400)
            self._mark_failure(job, state, wrapped.message)
            self.logger.error(
                "recovery_failed job_name=%s path=%s detail=%s",
                job.job_name, job.root_path, wrapped.message,
            )
            raise wrapped from error
        if isinstance(error, RecoveryError):
            self._mark_failure(job, state, error.message)
            self.logger.error(
                "recovery_failed job_name=%s path=%s detail=%s",
                job.job_name, job.root_path, error.message,
            )
            raise error
        self._mark_failure(job, state, str(error))
        self.logger.exception("recovery_failed job_name=%s path=%s", job.job_name, job.root_path)
        raise RecoveryError(str(error), status_code=500) from error

    def _mark_failure(self, job: JobDefinition, state, detail: str) -> None:
        state.job_name = job.job_name
        state.last_status = "recovery_failed"
        state.last_error_category = "recovery"
        state.last_error_detail = detail
        self.state_store.set(job.state_key, state)

    def _temp_path(self, suffix: str) -> Path:
        self.settings.spool_dir.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(
            dir=self.settings.spool_dir, delete=False, suffix=suffix
        ) as handle:
            return Path(handle.name)
