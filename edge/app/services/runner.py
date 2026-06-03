from __future__ import annotations

import threading

from app.backup.discovery import discover_jobs
from app.core.config import Settings, hook_scripts_dir
from app.core.logging import configure_logging
from app.services.directories import DirectoryService
from app.services.hooks import HookManager
from app.services.job_locks import JobLockManager
from app.services.job_processor import JobProcessor
from app.services.ntfy import NtfyPublisher
from app.services.recovery import RecoveryService
from app.services.settings_store import SettingsStore
from app.services.state import StateStore
from app.services.upload import UploadClient


class EdgeRunner:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.logger = configure_logging(settings.log_level)
        self.settings_store = SettingsStore()
        self.state_store = StateStore(settings.state_dir)
        self.upload_client = UploadClient(settings)
        self.lock_manager = JobLockManager()
        self.hook_manager = HookManager(hook_scripts_dir(), self.logger)
        self.ntfy_publisher = NtfyPublisher(self.logger)
        self._cycle_lock = threading.Lock()
        self._apply_settings(settings)

    def run_cycle(self) -> bool:
        if not self._cycle_lock.acquire(blocking=False):
            self.logger.info("cycle_already_running")
            return False

        try:
            return self.job_processor.run_cycle()
        finally:
            self._cycle_lock.release()

    def list_directories(self) -> list[dict]:
        return self.directory_service.list_directories()

    def save_job(self, relative_path: str, payload: dict) -> dict:
        return self.directory_service.save_job(relative_path, payload)

    def delete_job(self, relative_path: str) -> None:
        self.directory_service.delete_job(relative_path)

    def clear_manual_interventions(self) -> int:
        return self.state_store.clear_manual_interventions()

    def force_send_job(self, job_name: str) -> dict[str, object]:
        normalized_job_name = job_name.strip()
        if not normalized_job_name:
            raise ValueError("job_name is required")

        jobs = [
            job
            for job in discover_jobs(self.settings.scan_root, self.settings.max_depth, self.logger)
            if job.job_name == normalized_job_name
        ]
        if not jobs:
            raise ValueError("job not found")
        if len(jobs) > 1:
            raise ValueError("multiple jobs share that job_name")
        if not self._cycle_lock.acquire(blocking=False):
            self.logger.info("job_force_send_skipped job_name=%s reason=cycle_already_running", normalized_job_name)
            return {"status": "already_running", "job_name": normalized_job_name}

        try:
            cleared = self.state_store.clear_manual_intervention(jobs[0].state_key)
            self.job_processor.process_job(jobs[0], force_send=True)
            return {
                "status": "started",
                "job_name": normalized_job_name,
                "manual_retry_cleared": cleared,
            }
        finally:
            self._cycle_lock.release()

    def recover_job(self, relative_path: str, fingerprint: str | None = None) -> dict[str, object]:
        job = self.directory_service.load_job(relative_path)
        if not self._cycle_lock.acquire(blocking=False):
            self.logger.info("job_recovery_skipped path=%s reason=cycle_already_running", relative_path)
            return {"status": "already_running", "relative_path": relative_path}
        try:
            result = self.recovery_service.recover(job, fingerprint=fingerprint)
            result["relative_path"] = relative_path
            return result
        finally:
            self._cycle_lock.release()

    def preview_recovery(self, relative_path: str, fingerprint: str | None = None) -> dict[str, object]:
        job = self.directory_service.load_job(relative_path)
        if not self._cycle_lock.acquire(blocking=False):
            self.logger.info("job_recovery_preview_skipped path=%s reason=cycle_already_running", relative_path)
            return {"status": "already_running", "relative_path": relative_path}
        try:
            result = self.recovery_service.preview(job, fingerprint=fingerprint)
            result["relative_path"] = relative_path
            return result
        finally:
            self._cycle_lock.release()

    def save_settings(self, payload: dict) -> Settings:
        settings = self.settings_store.save(payload)
        self.update_settings(settings)
        return settings

    def update_settings(self, settings: Settings) -> None:
        if not self._cycle_lock.acquire(blocking=False):
            raise RuntimeError("cannot update settings while a backup cycle is running")
        try:
            self._apply_settings(settings)
        finally:
            self._cycle_lock.release()

    def _apply_settings(self, settings: Settings) -> None:
        self.settings = settings
        self.logger = configure_logging(settings.log_level)
        self.settings.state_dir.mkdir(parents=True, exist_ok=True)
        self.settings.spool_dir.mkdir(parents=True, exist_ok=True)
        self.state_store = StateStore(settings.state_dir)
        self.upload_client = UploadClient(settings)
        self.directory_service = DirectoryService(settings, self.logger, self.state_store)
        self.hook_manager.logger = self.logger
        self.ntfy_publisher.logger = self.logger
        self.job_processor = JobProcessor(
            settings=settings,
            logger=self.logger,
            state_store=self.state_store,
            upload_client=self.upload_client,
            lock_manager=self.lock_manager,
            hook_manager=self.hook_manager,
            ntfy_publisher=self.ntfy_publisher,
        )
        self.recovery_service = RecoveryService(
            settings=settings,
            logger=self.logger,
            state_store=self.state_store,
            upload_client=self.upload_client,
        )
