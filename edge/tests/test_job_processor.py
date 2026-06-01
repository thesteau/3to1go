from __future__ import annotations

import sys
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch


PROJECT_ROOT = Path(__file__).resolve().parents[1]
WORKSPACE_ROOT = PROJECT_ROOT.parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.backup.discovery import JobDefinition  # noqa: E402
from app.core.config import Settings  # noqa: E402
from app.services.job_locks import JobLockManager  # noqa: E402
from app.services import job_processor as job_processor_module  # noqa: E402
from app.services.job_processor import JobProcessor  # noqa: E402
from app.services.state import JobState, StateStore  # noqa: E402
from app.services.upload import UploadFailure, sha256_path  # noqa: E402


class _RetryableFailureUploadClient:
    def upload_archive(self, **_kwargs):
        raise UploadFailure(
            message="central unavailable",
            category="network",
            retryable=True,
            retry_after_seconds=30,
        )


class _CircuitOpenUploadClient:
    def upload_archive(self, **_kwargs):
        raise UploadFailure(
            message="central circuit breaker is open",
            category="circuit_open",
            retryable=True,
            retry_after_seconds=120,
        )


class JobProcessorTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-job-processor"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))
        self.settings = Settings(
            edge_id="edge-01",
            scan_root=self.temp_dir / "scan",
            central_url="http://central:6555",
            advertised_url="",
            auth_token="secret",
            cron_schedule="0 2 * * *",
            state_dir=self.temp_dir / "state",
            spool_dir=self.temp_dir / "spool",
            log_level="INFO",
            max_depth=5,
            keep_local_pending=False,
            upload_chunk_size_mb=8,
            min_upload_chunk_size_mb=1,
            max_upload_chunk_size_mb=16,
            upload_retry_max_attempts=5,
            upload_retry_base_delay_seconds=5,
            upload_retry_max_delay_seconds=300,
            upload_connect_timeout_seconds=10,
            upload_read_timeout_padding_seconds=30,
            upload_min_throughput_bytes_per_second=262144,
            circuit_breaker_failure_threshold=5,
            circuit_breaker_cooldown_seconds=300,
            ntfy_url="",
            ntfy_topic="",
            ntfy_message_template="",
            hook_pre_command="",
            hook_post_command="",
            http_host="127.0.0.1",
            http_port=6556,
        )
        self.settings.scan_root.mkdir(parents=True, exist_ok=True)
        self.state_store = StateStore(self.settings.state_dir)
        self.logger = Mock()
        self.processor = JobProcessor(
            settings=self.settings,
            logger=self.logger,
            state_store=self.state_store,
            upload_client=_RetryableFailureUploadClient(),
            quiescer=None,
            lock_manager=JobLockManager(),
            hook_manager=Mock(),
            ntfy_publisher=Mock(),
        )

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_retryable_failure_without_local_pending_waits_for_rebuild(self) -> None:
        job_root = self.settings.scan_root / "photos"
        job_root.mkdir(parents=True, exist_ok=True)
        job = JobDefinition(
            root_path=job_root,
            job_name="photos",
            exclude_patterns=[],
            include_hidden=True,
            follow_symlinks=False,
        )

        archive_path = self.settings.spool_dir / "photos__pending.tar.zst"
        archive_path.parent.mkdir(parents=True, exist_ok=True)
        archive_path.write_bytes(b"archive-bytes")

        state = JobState(
            job_name="photos",
            pending_archive=str(archive_path),
            pending_archive_size=archive_path.stat().st_size,
            pending_archive_sha256=sha256_path(archive_path),
            pending_fingerprint="abcdef1234567890",
            pending_timestamp="2026-04-05T12:00:00Z",
        )

        self.assertFalse(self.processor._upload_pending_archive(job, state))

        saved = self.state_store.get(job.state_key)
        self.assertIsNone(saved.pending_archive)
        self.assertEqual(saved.pending_fingerprint, "abcdef1234567890")
        self.assertEqual(saved.pending_timestamp, "2026-04-05T12:00:00Z")
        self.assertEqual(saved.last_status, "retry_scheduled")
        self.assertIsNotNone(saved.next_retry_at)
        self.assertFalse(saved.manual_intervention_required)
        self.assertFalse(archive_path.exists())

        self.assertTrue(self.processor._retry_pending_if_needed(job, saved))
        waiting = self.state_store.get(job.state_key)
        self.assertEqual(waiting.last_status, "waiting_retry")
        self.assertIsNone(waiting.pending_archive)
        self.assertEqual(waiting.pending_fingerprint, "abcdef1234567890")

    def test_circuit_open_failure_schedules_retry_without_manual_intervention(self) -> None:
        self.processor.upload_client = _CircuitOpenUploadClient()
        job_root = self.settings.scan_root / "photos"
        job_root.mkdir(parents=True, exist_ok=True)
        job = JobDefinition(
            root_path=job_root,
            job_name="photos",
            exclude_patterns=[],
            include_hidden=True,
            follow_symlinks=False,
        )

        archive_path = self.settings.spool_dir / "photos__pending.tar.zst"
        archive_path.parent.mkdir(parents=True, exist_ok=True)
        archive_path.write_bytes(b"archive-bytes")

        state = JobState(
            job_name="photos",
            pending_archive=str(archive_path),
            pending_archive_size=archive_path.stat().st_size,
            pending_archive_sha256=sha256_path(archive_path),
            pending_fingerprint="abcdef1234567890",
            pending_timestamp="2026-04-05T12:00:00Z",
        )

        self.assertFalse(self.processor._upload_pending_archive(job, state))

        saved = self.state_store.get(job.state_key)
        self.assertEqual(saved.last_status, "circuit_open")
        self.assertEqual(saved.last_error_category, "circuit_open")
        self.assertEqual(saved.last_error_detail, "central circuit breaker is open")
        self.assertFalse(saved.manual_intervention_required)
        self.assertIsNotNone(saved.next_retry_at)

    def test_force_send_rebuilds_even_when_fingerprint_is_unchanged(self) -> None:
        job_root = self.settings.scan_root / "photos"
        job_root.mkdir(parents=True, exist_ok=True)
        job = JobDefinition(
            root_path=job_root,
            job_name="photos",
            exclude_patterns=[],
            include_hidden=True,
            follow_symlinks=False,
        )
        archive_path = self.settings.spool_dir / "photos__forced.tar.zst"
        archive_path.parent.mkdir(parents=True, exist_ok=True)
        archive_path.write_bytes(b"archive-bytes")
        self.state_store.set(
            job.state_key,
            JobState(
                job_name="photos",
                last_successful_fingerprint="abcdef1234567890",
            ),
        )

        with (
            patch.object(job_processor_module, "build_file_list", return_value=[object()]),
            patch.object(job_processor_module, "compute_fingerprint", return_value="abcdef1234567890"),
            patch.object(
                self.processor,
                "_create_pending_archive",
                return_value=(archive_path, "2026-04-05T12:00:00Z"),
            ) as create_archive,
            patch.object(self.processor, "_upload_pending_archive", return_value=True) as upload_pending,
        ):
            self.processor._process_job_locked(job, force_send=True)

        create_archive.assert_called_once()
        upload_pending.assert_called_once()
        saved = self.state_store.get(job.state_key)
        self.assertEqual(saved.last_status, "archive_created")
        self.assertEqual(saved.pending_archive, str(archive_path))
        self.assertEqual(saved.pending_fingerprint, "abcdef1234567890")


if __name__ == "__main__":
    unittest.main()
