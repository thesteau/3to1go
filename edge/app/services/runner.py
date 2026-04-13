from __future__ import annotations

import threading

from app.backup.quiesce import DockerComposeQuiescer
from app.core.config import Settings
from app.core.logging import configure_logging
from app.services.directories import DirectoryService
from app.services.job_locks import JobLockManager
from app.services.job_processor import JobProcessor
from app.services.state import StateStore
from app.services.upload import UploadClient


class EdgeRunner:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.logger = configure_logging(settings.log_level)
        self.state_store = StateStore(settings.state_dir)
        self.upload_client = UploadClient(settings)
        self.quiescer = DockerComposeQuiescer(self.logger)
        self.lock_manager = JobLockManager()
        self._cycle_lock = threading.Lock()
        self.settings.state_dir.mkdir(parents=True, exist_ok=True)
        self.settings.spool_dir.mkdir(parents=True, exist_ok=True)
        self.directory_service = DirectoryService(settings, self.logger, self.state_store)
        self.job_processor = JobProcessor(
            settings=settings,
            logger=self.logger,
            state_store=self.state_store,
            upload_client=self.upload_client,
            quiescer=self.quiescer,
            lock_manager=self.lock_manager,
        )

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
