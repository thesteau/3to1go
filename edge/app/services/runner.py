from __future__ import annotations

import os
import threading
from dataclasses import asdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from app.backup.archiver import build_archive_name, create_archive, timestamp_for_api
from app.backup.discovery import (
    UPLOAD_DIR_FILENAME,
    JobDefinition,
    build_job_definition,
    delete_upload_dir,
    discover_jobs,
    job_definition_to_payload,
    read_upload_dir_payload,
    write_upload_dir,
)
from app.backup.filters import build_file_list
from app.backup.fingerprint import compute_fingerprint
from app.backup.quiesce import DockerComposeQuiescer, QuiesceContext
from app.core.config import Settings
from app.core.logging import configure_logging
from app.core.schedule import MINIMUM_SCHEDULE_MINUTES
from app.services.state import JobState, StateStore
from app.services.upload import UploadClient


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
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.logger = configure_logging(settings.log_level)
        self.state_store = StateStore(settings.state_dir)
        self.upload_client = UploadClient(settings.central_url, settings.auth_token)
        self.lock_manager = JobLockManager()
        self.quiescer = DockerComposeQuiescer(self.logger)
        self._cycle_lock = threading.Lock()
        self.settings.state_dir.mkdir(parents=True, exist_ok=True)
        self.settings.spool_dir.mkdir(parents=True, exist_ok=True)
        self.settings.scan_root.mkdir(parents=True, exist_ok=True)

    def run_cycle(self) -> bool:
        if not self._cycle_lock.acquire(blocking=False):
            self.logger.info("cycle_already_running")
            return False

        try:
            for job in discover_jobs(self.settings.scan_root, self.settings.max_depth, self.logger):
                self.process_job(job)
            self.cleanup_stale_archives()
            return True
        finally:
            self._cycle_lock.release()

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

    def list_directories(self) -> list[dict[str, Any]]:
        scan_root = self.settings.scan_root.resolve()
        entries: list[dict[str, Any]] = []

        def walk(directory: Path, depth: int, blocked_by: str | None) -> None:
            if depth > self.settings.max_depth:
                return

            relative_path = "." if directory == scan_root else directory.relative_to(scan_root).as_posix()
            marker_path = directory / UPLOAD_DIR_FILENAME
            has_marker = marker_path.is_file()
            config_error: str | None = None
            config: dict[str, Any] | None = None

            if has_marker:
                try:
                    payload = read_upload_dir_payload(marker_path)
                    config = job_definition_to_payload(build_job_definition(directory, payload))
                except (ValueError, OSError) as exc:
                    config_error = str(exc)

            state = self.state_store.get(str(directory.resolve()))
            entries.append(
                {
                    "relative_path": relative_path,
                    "absolute_path": str(directory),
                    "selected": has_marker,
                    "blocked_by_parent": blocked_by,
                    "config": config,
                    "config_error": config_error,
                    "state": asdict(state),
                }
            )

            try:
                children = sorted(
                    [Path(entry.path) for entry in os.scandir(directory) if entry.is_dir(follow_symlinks=False)],
                    key=lambda item: item.name.lower(),
                )
            except (FileNotFoundError, NotADirectoryError, PermissionError, OSError):
                return

            child_blocked_by = relative_path if has_marker else blocked_by
            for child in children:
                walk(child, depth + 1, child_blocked_by)

        walk(scan_root, 0, None)
        return entries

    def save_job(self, relative_path: str, payload: dict[str, Any]) -> dict[str, Any]:
        directory = self.resolve_directory(relative_path)
        blocking_ancestor = self.find_blocking_ancestor(directory)
        if blocking_ancestor is not None and not (directory / UPLOAD_DIR_FILENAME).exists():
            raise ValueError(f"directory is nested under existing job {blocking_ancestor}")

        write_upload_dir(directory, payload)
        job = build_job_definition(directory, read_upload_dir_payload(directory / UPLOAD_DIR_FILENAME))
        self.logger.info("ui_job_saved path=%s job_name=%s", directory, job.job_name)
        return self.serialize_directory(directory)

    def delete_job(self, relative_path: str) -> None:
        directory = self.resolve_directory(relative_path)
        delete_upload_dir(directory)
        self.state_store.delete(str(directory.resolve()))
        self.logger.info("ui_job_deleted path=%s", directory)

    def serialize_directory(self, directory: Path) -> dict[str, Any]:
        marker_path = directory / UPLOAD_DIR_FILENAME
        config: dict[str, Any] | None = None
        config_error: str | None = None
        if marker_path.exists():
            try:
                payload = read_upload_dir_payload(marker_path)
                config = job_definition_to_payload(build_job_definition(directory, payload))
            except (ValueError, OSError) as exc:
                config_error = str(exc)

        state = self.state_store.get(str(directory.resolve()))
        relative_path = "." if directory.resolve() == self.settings.scan_root.resolve() else directory.resolve().relative_to(self.settings.scan_root.resolve()).as_posix()
        return {
            "relative_path": relative_path,
            "absolute_path": str(directory.resolve()),
            "selected": marker_path.exists(),
            "blocked_by_parent": self.find_blocking_ancestor(directory),
            "config": config,
            "config_error": config_error,
            "state": asdict(state),
        }

    def resolve_directory(self, relative_path: str) -> Path:
        candidate = (self.settings.scan_root / relative_path).resolve() if relative_path != "." else self.settings.scan_root.resolve()
        try:
            candidate.relative_to(self.settings.scan_root.resolve())
        except ValueError as exc:
            raise ValueError("path must remain within scan root") from exc
        if not candidate.exists() or not candidate.is_dir():
            raise ValueError("directory not found")
        return candidate

    def find_blocking_ancestor(self, directory: Path) -> str | None:
        resolved_root = self.settings.scan_root.resolve()
        resolved_directory = directory.resolve()
        if resolved_directory == resolved_root:
            return None

        current = resolved_directory.parent
        while True:
            if current == resolved_root.parent:
                return None
            if (current / UPLOAD_DIR_FILENAME).exists() and current != resolved_directory:
                return "." if current == resolved_root else current.relative_to(resolved_root).as_posix()
            if current == resolved_root:
                return None
            current = current.parent



def build_directory_response(runner: EdgeRunner) -> dict[str, Any]:
    return {
        "edge_id": runner.settings.edge_id,
        "scan_root": str(runner.settings.scan_root),
        "central_url": runner.settings.central_url,
        "cron_schedule": runner.settings.cron_schedule,
        "minimum_cycle_gap_minutes": MINIMUM_SCHEDULE_MINUTES,
        "http_url": f"http://localhost:{runner.settings.http_port}",
        "directories": runner.list_directories(),
    }
