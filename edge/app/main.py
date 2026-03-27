from __future__ import annotations

import argparse
import threading
from datetime import datetime, timezone
from pathlib import Path

from app.archiver import build_archive_name, create_archive, timestamp_for_api
from app.config import Settings, load_settings
from app.discovery import JobDefinition, discover_jobs
from app.filters import build_file_list
from app.fingerprint import compute_fingerprint
from app.scheduler import run_scheduler
from app.state import JobState, StateStore
from app.upload import UploadClient
from app.utils.logging import configure_logging


class JobLockManager:
    def __init__(self) -> None:
        self._locks: dict[str, threading.Lock] = {}
        self._manager_lock = threading.Lock()

    def acquire(self, key: str) -> threading.Lock | None:
        with self._manager_lock:
            lock = self._locks.setdefault(key, threading.Lock())
        if not lock.acquire(blocking=False):
            return None
        return lock


class EdgeRunner:
    def __init__(self, settings: Settings, dry_run: bool = False) -> None:
        self.settings = settings
        self.dry_run = dry_run
        self.logger = configure_logging(settings.log_level)
        self.state_store = StateStore(settings.state_dir)
        self.upload_client = UploadClient(settings.central_url, settings.auth_token)
        self.lock_manager = JobLockManager()
        self.settings.state_dir.mkdir(parents=True, exist_ok=True)
        self.settings.spool_dir.mkdir(parents=True, exist_ok=True)

    def run_cycle(self) -> None:
        jobs = discover_jobs(
            scan_root=self.settings.scan_root,
            max_depth=self.settings.max_depth,
            logger=self.logger,
        )
        for job in jobs:
            self.process_job(job)
        self.cleanup_stale_archives()

    def process_job(self, job: JobDefinition) -> None:
        lock = self.lock_manager.acquire(job.state_key)
        if lock is None:
            self.logger.info("job_locked job_name=%s path=%s", job.job_name, job.root_path)
            return

        try:
            self._process_job_locked(job)
        except Exception:
            self.logger.exception(
                "unexpected_exception job_name=%s path=%s",
                job.job_name,
                job.root_path,
            )
            current_state = self.state_store.get(job.state_key)
            current_state.job_name = job.job_name
            current_state.last_status = "unexpected_exception"
            self.state_store.set(job.state_key, current_state)
        finally:
            lock.release()

    def _process_job_locked(self, job: JobDefinition) -> None:
        state = self.state_store.get(job.state_key)

        if self._retry_pending_if_needed(job, state):
            return

        state.job_name = job.job_name
        files = build_file_list(job, self.logger)
        if not files:
            state.last_status = "skipped_empty"
            self.state_store.set(job.state_key, state)
            self.logger.info("skipped_empty job_name=%s path=%s", job.job_name, job.root_path)
            return

        fingerprint = compute_fingerprint(files)
        if fingerprint == state.last_successful_fingerprint:
            state.last_status = "skipped_unchanged"
            self.state_store.set(job.state_key, state)
            self.logger.info(
                "skipped_unchanged job_name=%s fingerprint=%s",
                job.job_name,
                fingerprint[:8],
            )
            return

        now = datetime.now(timezone.utc)
        timestamp = timestamp_for_api(now)
        archive_name = build_archive_name(job.job_name, now, fingerprint)
        archive_path = self.settings.spool_dir / archive_name

        if self.dry_run:
            state.last_status = "dry_run"
            self.state_store.set(job.state_key, state)
            self.logger.info(
                "dry_run_upload job_name=%s fingerprint=%s archive=%s",
                job.job_name,
                fingerprint[:8],
                archive_name,
            )
            return

        create_archive(archive_path=archive_path, files=files)
        self.logger.info("archive_created job_name=%s archive=%s", job.job_name, archive_name)

        state.pending_archive = str(archive_path)
        state.pending_fingerprint = fingerprint
        state.pending_timestamp = timestamp
        state.job_name = job.job_name
        state.last_status = "archive_created"
        self.state_store.set(job.state_key, state)

        self._upload_pending_archive(job, state)

    def _retry_pending_if_needed(self, job: JobDefinition, state: JobState) -> bool:
        if not state.pending_archive or not state.pending_fingerprint or not state.pending_timestamp:
            return False

        pending_path = Path(state.pending_archive)
        if not pending_path.exists():
            state.pending_archive = None
            state.pending_fingerprint = None
            state.pending_timestamp = None
            state.last_status = "skipped_missing"
            self.state_store.set(job.state_key, state)
            self.logger.warning(
                "skipped_missing job_name=%s pending_archive=%s",
                state.job_name or job.job_name,
                pending_path,
            )
            return False

        self.logger.info(
            "retry_pending job_name=%s archive=%s",
            state.job_name or job.job_name,
            pending_path.name,
        )
        self._upload_pending_archive(job, state)
        return True

    def _upload_pending_archive(self, job: JobDefinition, state: JobState) -> None:
        if not state.pending_archive or not state.pending_fingerprint or not state.pending_timestamp:
            return

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
            self.logger.error(
                "upload_failure job_name=%s archive=%s detail=%s",
                upload_job_name,
                archive_path.name,
                exc,
            )
            state.last_status = "upload_failure"
            if not self.settings.keep_local_pending:
                archive_path.unlink(missing_ok=True)
                state.pending_archive = None
                state.pending_fingerprint = None
                state.pending_timestamp = None
            self.state_store.set(job.state_key, state)
            return

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

    def cleanup_stale_archives(self) -> None:
        referenced = self.state_store.referenced_pending_archives()
        for path in self.settings.spool_dir.glob("*.tar.zst"):
            if str(path) not in referenced:
                path.unlink(missing_ok=True)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="RelayCentralizer Edge")
    parser.add_argument("--once", action="store_true", help="Run one scan/upload cycle and exit")
    parser.add_argument("--dry-run", action="store_true", help="Discover and fingerprint jobs without archiving or uploading")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    settings = load_settings()
    runner = EdgeRunner(settings=settings, dry_run=args.dry_run)

    if args.once:
        runner.run_cycle()
        return

    run_scheduler(runner.run_cycle, settings.interval_seconds, runner.logger)


if __name__ == "__main__":
    main()
