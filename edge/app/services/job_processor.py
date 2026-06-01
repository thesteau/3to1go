from __future__ import annotations

from datetime import datetime, timedelta, timezone
from pathlib import Path

from app.backup.archiver import build_archive_name, create_archive, timestamp_for_api
from app.backup.discovery import JobDefinition, discover_jobs
from app.backup.filters import build_file_list
from app.backup.fingerprint import compute_fingerprint
from app.backup.quiesce import DockerComposeQuiescer, QuiesceContext
from app.core.config import Settings, encryption_key_path, installation_id_path
from app.core.encryption import encrypt_file, load_or_create_key
from app.core.identity import load_or_create_installation_id
from app.services.hooks import HookManager
from app.services.job_locks import JobLockManager
from app.services.ntfy import NtfyPublisher
from app.services.state import JobState, StateStore
from app.services.upload import UploadClient, UploadFailure, sha256_path


class JobProcessor:
    def __init__(
        self,
        settings: Settings,
        logger,
        state_store: StateStore,
        upload_client: UploadClient,
        quiescer: DockerComposeQuiescer,
        lock_manager: JobLockManager,
        hook_manager: HookManager,
        ntfy_publisher: NtfyPublisher,
    ) -> None:
        self.settings = settings
        self.logger = logger
        self.state_store = state_store
        self.upload_client = upload_client
        self.quiescer = quiescer
        self.lock_manager = lock_manager
        self.hook_manager = hook_manager
        self.ntfy_publisher = ntfy_publisher

    def run_cycle(self) -> bool:
        if not self.settings.auth_token.strip():
            self.logger.warning("cycle_skipped reason=auth_token_missing")
            return False
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
            pre_state = self.state_store.get(job.state_key)
            pre_state.job_name = job.job_name
            self.hook_manager.run_command(
                self.settings.hook_pre_command,
                phase="pre",
                context=self._hook_context(job, pre_state),
            )
            self._process_job_locked(job)
        except Exception as exc:
            self.logger.exception("unexpected_exception job_name=%s path=%s", job.job_name, job.root_path)
            current_state = self.state_store.get(job.state_key)
            current_state.job_name = job.job_name
            current_state.last_status = "unexpected_exception"
            current_state.last_error_category = "unexpected"
            current_state.last_error_detail = str(exc)
            self.state_store.set(job.state_key, current_state)
        finally:
            final_state = self.state_store.get(job.state_key)
            final_state.job_name = job.job_name
            hook_context = self._hook_context(job, final_state)
            self.hook_manager.run_command(self.settings.hook_post_command, phase="post", context=hook_context)
            if final_state.last_status == "success":
                self.ntfy_publisher.publish_best_effort(self.settings, hook_context)
            lock.release()

    def _process_job_locked(self, job: JobDefinition) -> None:
        state = self.state_store.get(job.state_key)
        state.job_name = job.job_name

        if not state.manual_intervention_required and self._retry_pending_if_needed(job, state):
            return

        files = build_file_list(job, self.logger)
        if not files:
            self._clear_pending_archive(state)
            state.last_status = "skipped_empty"
            state.last_error_category = None
            state.last_error_detail = None
            state.manual_intervention_required = False
            self.state_store.set(job.state_key, state)
            self.logger.info("skipped_empty job_name=%s path=%s", job.job_name, job.root_path)
            return

        fingerprint = compute_fingerprint(files)
        if fingerprint == state.last_successful_fingerprint:
            self._clear_pending_archive(state)
            state.last_status = "skipped_unchanged"
            state.last_error_category = None
            state.last_error_detail = None
            state.manual_intervention_required = False
            self.state_store.set(job.state_key, state)
            self.logger.info("skipped_unchanged job_name=%s fingerprint=%s", job.job_name, fingerprint[:8])
            return

        if (
            state.manual_intervention_required
            and state.pending_archive
            and state.pending_fingerprint == fingerprint
            and Path(state.pending_archive).exists()
        ):
            state.last_status = "manual_intervention_required"
            self.state_store.set(job.state_key, state)
            self.logger.warning(
                "manual_intervention_required job_name=%s archive=%s detail=%s",
                job.job_name,
                Path(state.pending_archive).name,
                state.last_error_detail,
            )
            return

        archive_path, timestamp = self._create_pending_archive(job, files, fingerprint)
        previous_pending_archive = state.pending_archive
        state.pending_archive = str(archive_path)
        state.pending_archive_size = archive_path.stat().st_size
        state.pending_archive_sha256 = sha256_path(archive_path)
        state.pending_fingerprint = fingerprint
        state.pending_timestamp = timestamp
        state.upload_id = None
        state.upload_offset = 0
        state.upload_attempt_count = 0
        state.current_chunk_size_bytes = None
        state.next_retry_at = None
        state.last_error_detail = None
        state.last_error_category = None
        state.last_stored_as = None
        state.last_pruned = 0
        state.last_duplicate = False
        state.last_upload_started_at = None
        state.last_upload_updated_at = None
        state.manual_intervention_required = False
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

        key = load_or_create_key(encryption_key_path())
        tmp_path = archive_path.with_suffix(".enc.tmp")
        encrypt_file(key, archive_path, tmp_path)
        archive_path.unlink()
        tmp_path.rename(archive_path)

        self.logger.info("archive_created job_name=%s archive=%s", job.job_name, archive_name)
        return archive_path, timestamp

    def _retry_pending_if_needed(self, job: JobDefinition, state: JobState) -> bool:
        if not state.pending_fingerprint:
            return False

        retry_at = _parse_utc_text(state.next_retry_at)
        if not state.pending_archive:
            if retry_at is not None and retry_at > datetime.now(timezone.utc):
                state.last_status = "waiting_retry"
                self.state_store.set(job.state_key, state)
                self.logger.info(
                    "waiting_retry job_name=%s archive=%s retry_at=%s",
                    state.job_name or job.job_name,
                    "rebuild_required",
                    state.next_retry_at,
                )
                return True
            return False

        pending_path = Path(state.pending_archive)
        if not pending_path.exists():
            self._clear_pending_archive(state)
            state.last_status = "skipped_missing"
            self.state_store.set(job.state_key, state)
            self.logger.warning(
                "skipped_missing job_name=%s pending_archive=%s", state.job_name or job.job_name, pending_path
            )
            return False

        if retry_at is not None and retry_at > datetime.now(timezone.utc):
            state.last_status = "waiting_retry"
            self.state_store.set(job.state_key, state)
            self.logger.info(
                "waiting_retry job_name=%s archive=%s retry_at=%s",
                state.job_name or job.job_name,
                pending_path.name,
                state.next_retry_at,
            )
            return True

        self.logger.info("retry_pending job_name=%s archive=%s", state.job_name or job.job_name, pending_path.name)
        return self._upload_pending_archive(job, state)

    def _upload_pending_archive(self, job: JobDefinition, state: JobState) -> bool:
        if not state.pending_archive or not state.pending_fingerprint or not state.pending_timestamp:
            return False

        archive_path = Path(state.pending_archive)
        if not archive_path.exists():
            self._clear_pending_archive(state)
            state.last_status = "skipped_missing"
            self.state_store.set(job.state_key, state)
            return False

        upload_job_name = state.job_name or job.job_name
        if state.pending_archive_size is None:
            state.pending_archive_size = archive_path.stat().st_size
        if state.pending_archive_sha256 is None:
            state.pending_archive_sha256 = sha256_path(archive_path)

        state.last_status = "uploading"
        state.last_upload_started_at = _utc_now_text()
        state.last_upload_updated_at = state.last_upload_started_at
        state.next_retry_at = None
        state.manual_intervention_required = False
        self.state_store.set(job.state_key, state)

        def persist_progress(upload_id: str, offset: int, chunk_size: int) -> None:
            state.upload_id = upload_id
            state.upload_offset = offset
            state.current_chunk_size_bytes = chunk_size
            state.last_upload_updated_at = _utc_now_text()
            state.last_status = "uploading"
            self.state_store.set(job.state_key, state)

        try:
            response = self.upload_client.upload_archive(
                edge_id=self.settings.edge_id,
                job_name=upload_job_name,
                fingerprint=state.pending_fingerprint,
                timestamp=state.pending_timestamp,
                archive_path=archive_path,
                archive_sha256=state.pending_archive_sha256,
                upload_id=state.upload_id,
                upload_offset=state.upload_offset,
                preferred_chunk_size=state.current_chunk_size_bytes,
                progress_callback=persist_progress,
            )
        except UploadFailure as exc:
            self.logger.error(
                "upload_failure job_name=%s archive=%s category=%s detail=%s",
                upload_job_name,
                archive_path.name,
                exc.category,
                exc,
            )
            state.upload_attempt_count += 1
            state.last_error_detail = str(exc)
            state.last_error_category = exc.category
            if exc.retryable:
                state.last_status = "circuit_open" if exc.category == "circuit_open" else "retry_scheduled"
                state.next_retry_at = _utc_after_seconds_text(
                    self._retry_delay_seconds(state.upload_attempt_count, exc)
                )
                state.manual_intervention_required = False
            else:
                state.last_status = "manual_intervention_required"
                state.next_retry_at = None
                state.manual_intervention_required = True
            state.last_upload_updated_at = _utc_now_text()
            if exc.retryable and not self.settings.keep_local_pending:
                self._discard_pending_archive_file(state)
            self.state_store.set(job.state_key, state)
            return False
        except Exception as exc:
            self.logger.error(
                "upload_failure job_name=%s archive=%s detail=%s", upload_job_name, archive_path.name, exc
            )
            state.upload_attempt_count += 1
            state.last_error_detail = str(exc)
            state.last_error_category = "unexpected"
            state.last_status = "retry_scheduled"
            state.next_retry_at = _utc_after_seconds_text(self._retry_delay_seconds(state.upload_attempt_count, None))
            state.last_upload_updated_at = _utc_now_text()
            if not self.settings.keep_local_pending:
                self._discard_pending_archive_file(state)
            self.state_store.set(job.state_key, state)
            return False

        archive_path.unlink(missing_ok=True)
        completed_at = _utc_now_text()
        state.last_successful_fingerprint = state.pending_fingerprint
        state.last_successful_upload = state.pending_timestamp
        state.last_error_detail = None
        state.last_error_category = None
        state.upload_attempt_count = 0
        state.last_status = "success"
        state.last_stored_as = str(response.get("stored_as") or "")
        state.last_pruned = int(response.get("pruned") or 0)
        state.last_duplicate = bool(response.get("duplicate", False))
        state.next_retry_at = None
        state.manual_intervention_required = False
        self._clear_pending_archive(state)
        state.last_upload_updated_at = completed_at
        self.state_store.set(job.state_key, state)
        self.logger.info(
            "upload_success job_name=%s archive=%s stored_as=%s pruned=%s duplicate=%s",
            upload_job_name,
            archive_path.name,
            response.get("stored_as"),
            response.get("pruned"),
            response.get("duplicate", False),
        )
        return True

    def _retry_delay_seconds(self, attempt_count: int, exc: UploadFailure | None) -> int:
        if exc is not None and exc.retry_after_seconds is not None:
            return exc.retry_after_seconds
        delay = self.settings.upload_retry_base_delay_seconds * (2 ** max(0, attempt_count - 1))
        delay = min(self.settings.upload_retry_max_delay_seconds, delay)
        if exc is not None and not exc.retryable:
            delay = max(delay, self.settings.upload_retry_base_delay_seconds * 6)
        return delay

    def _clear_pending_archive(self, state: JobState) -> None:
        if state.pending_archive:
            Path(state.pending_archive).unlink(missing_ok=True)
        state.pending_archive = None
        state.pending_archive_size = None
        state.pending_archive_sha256 = None
        state.pending_fingerprint = None
        state.pending_timestamp = None
        state.upload_id = None
        state.upload_offset = 0
        state.current_chunk_size_bytes = None
        state.next_retry_at = None

    def _discard_pending_archive_file(self, state: JobState) -> None:
        if state.pending_archive:
            Path(state.pending_archive).unlink(missing_ok=True)
        state.pending_archive = None
        state.pending_archive_size = None
        state.pending_archive_sha256 = None
        state.upload_id = None
        state.upload_offset = 0
        state.current_chunk_size_bytes = None

    def cleanup_stale_archives(self) -> None:
        referenced = self.state_store.referenced_pending_archives()
        for path in self.settings.spool_dir.glob("*.tar.zst"):
            if str(path) not in referenced:
                path.unlink(missing_ok=True)

    def _hook_context(self, job: JobDefinition, state: JobState) -> dict[str, str | int | bool | None]:
        return {
            "edge_id": self.settings.edge_id,
            "edge_instance_id": load_or_create_installation_id(installation_id_path()),
            "job_name": job.job_name,
            "job_root": str(job.root_path),
            "state_key": job.state_key,
            "last_status": state.last_status,
            "last_error_category": state.last_error_category,
            "last_error_detail": state.last_error_detail,
            "stored_as": state.last_stored_as,
            "pruned": state.last_pruned,
            "duplicate": state.last_duplicate,
            "pending_archive": state.pending_archive,
            "pending_fingerprint": state.pending_fingerprint,
            "pending_timestamp": state.pending_timestamp,
            "upload_id": state.upload_id,
            "upload_offset": state.upload_offset,
            "next_retry_at": state.next_retry_at,
        }


def _parse_utc_text(value: str | None) -> datetime | None:
    if not value:
        return None
    try:
        return datetime.strptime(value, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)
    except ValueError:
        return None


def _utc_now_text() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _utc_after_seconds_text(seconds: int) -> str:
    return (datetime.now(timezone.utc) + timedelta(seconds=seconds)).strftime("%Y-%m-%dT%H:%M:%SZ")
