from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path

from app.backup.archiver import build_archive_name, create_archive, timestamp_for_api
from app.backup.discovery import JobDefinition, discover_jobs
from app.backup.filters import build_file_list
from app.backup.fingerprint import compute_fingerprint
from app.backup.quiesce import DockerComposeQuiescer, QuiesceContext
from app.core.config import Settings
from app.services.job_locks import JobLockManager
from app.services.state import JobState, StateStore
from app.services.upload import UploadClient


class JobProcessor:
    def __init__(
        self,
        settings: Settings,
        logger,
        state_store: StateStore,
        upload_client: UploadClient,
        quiescer: DockerComposeQuiescer,
        lock_manager: JobLockManager,
    ) -> None:
        self.settings = settings
        self.logger = logger
        self.state_store = state_store
        self.upload_client = upload_client
        self.quiescer = quiescer
        self.lock_manager = lock_manager

    def run_cycle(self) -> bool:
        for job in discover_jobs(self.settings.scan_root, self.settings.max_depth, self.logger):
            self.process_job(job)
        self.cleanup_stale_archives()
        return True

    def process_job(self, job: JobDefinition) -> None:
        lock = self.lock_manager.acquire(job.state_key)
        if lock is None:
            self.logger.info("job_locked job_name=%s path=%s", job.job_name, job.root_path)
            return

        try:
            self._process_job_locked(job)
        except Exception:
            self.logger.exception("unexpected_exception job_name=%s path=%s", job.job_name, job.root_path)
            current_state = self.state_store.get(job.state_key)
            current_state.job_name = job.job_name
            current_state.last_status = "unexpected_exception"
            self.state_store.set(job.state_key, current_state)
        finally:
            lock.release()

    def _process_job_locked(self, job: JobDefinition) -> None:
        state = self.state_store.get(job.state_key)
        state.job_name = job.job_name

        if self._retry_pending_if_needed(job, state):
            return

        files = build_file_list(job, self.logger)
        if not files:
            self._clear_pending_archive(state)
            state.last_status = "skipped_empty"
            self.state_store.set(job.state_key, state)
            self.logger.info("skipped_empty job_name=%s path=%s", job.job_name, job.root_path)
            return

        fingerprint = compute_fingerprint(files)
        if fingerprint == state.last_successful_fingerprint:
            self._clear_pending_archive(state)
            state.last_status = "skipped_unchanged"
            self.state_store.set(job.state_key, state)
            self.logger.info("skipped_unchanged job_name=%s fingerprint=%s", job.job_name, fingerprint[:8])
            return

        if state.pending_archive and state.pending_fingerprint == fingerprint and Path(state.pending_archive).exists():
            state.last_status = "upload_failure"
            self.state_store.set(job.state_key, state)
            self.logger.info(
                "pending_archive_preserved job_name=%s archive=%s fingerprint=%s",
                job.job_name,
                Path(state.pending_archive).name,
                fingerprint[:8],
            )
            return

        archive_path, timestamp = self._create_pending_archive(job, files, fingerprint)
        previous_pending_archive = state.pending_archive
        state.pending_archive = str(archive_path)
        state.pending_fingerprint = fingerprint
        state.pending_timestamp = timestamp
        state.last_status = "archive_created"
        self.state_store.set(job.state_key, state)

        if previous_pending_archive and previous_pending_archive != str(archive_path):
            Path(previous_pending_archive).unlink(missing_ok=True)

        self._upload_pending_archive(job, state)

    def _create_pending_archive(self, job: JobDefinition, files: list, fingerprint: str) -> tuple[Path, str]:
        now = datetime.now(timezone.utc)
        timestamp = timestamp_for_api(now)
        archive_name = build_archive_name(job.job_name, now, fingerprint)
        archive_path = self.settings.spool_dir / archive_name

        quiesce_context: QuiesceContext | None = None
        try:
            quiesce_context = self.quiescer.prepare(job)
            create_archive(archive_path=archive_path, files=files)
        finally:
            self.quiescer.restore(job, quiesce_context)

        self.logger.info("archive_created job_name=%s archive=%s", job.job_name, archive_name)
        return archive_path, timestamp

    def _retry_pending_if_needed(self, job: JobDefinition, state: JobState) -> bool:
        if not state.pending_archive or not state.pending_fingerprint or not state.pending_timestamp:
            return False

        pending_path = Path(state.pending_archive)
        if not pending_path.exists():
            self._clear_pending_archive(state)
            state.last_status = "skipped_missing"
            self.state_store.set(job.state_key, state)
            self.logger.warning("skipped_missing job_name=%s pending_archive=%s", state.job_name or job.job_name, pending_path)
            return False

        self.logger.info("retry_pending job_name=%s archive=%s", state.job_name or job.job_name, pending_path.name)
        return self._upload_pending_archive(job, state)

    def _upload_pending_archive(self, job: JobDefinition, state: JobState) -> bool:
        if not state.pending_archive or not state.pending_fingerprint or not state.pending_timestamp:
            return False

        archive_path = Path(state.pending_archive)
        upload_job_name = state.job_name or job.job_name
        try:
            response = self.upload_client.upload_archive(
                edge_id=self.settings.edge_id,
                job_name=upload_job_name,
                fingerprint=state.pending_fingerprint,
                timestamp=state.pending_timestamp,
                archive_path=archive_path,
            )
        except Exception as exc:
            self.logger.error("upload_failure job_name=%s archive=%s detail=%s", upload_job_name, archive_path.name, exc)
            state.last_status = "upload_failure"
            if not self.settings.keep_local_pending:
                self._clear_pending_archive(state)
            self.state_store.set(job.state_key, state)
            return False

        archive_path.unlink(missing_ok=True)
        state.last_successful_fingerprint = state.pending_fingerprint
        state.last_successful_upload = state.pending_timestamp
        state.pending_archive = None
        state.pending_fingerprint = None
        state.pending_timestamp = None
        state.last_status = "success"
        self.state_store.set(job.state_key, state)
        self.logger.info(
            "upload_success job_name=%s archive=%s stored_as=%s pruned=%s",
            upload_job_name,
            archive_path.name,
            response.get("stored_as"),
            response.get("pruned"),
        )
        return True

    def _clear_pending_archive(self, state: JobState) -> None:
        if state.pending_archive:
            Path(state.pending_archive).unlink(missing_ok=True)
        state.pending_archive = None
        state.pending_fingerprint = None
        state.pending_timestamp = None

    def cleanup_stale_archives(self) -> None:
        referenced = self.state_store.referenced_pending_archives()
        for path in self.settings.spool_dir.glob("*.tar.zst"):
            if str(path) not in referenced:
                path.unlink(missing_ok=True)
