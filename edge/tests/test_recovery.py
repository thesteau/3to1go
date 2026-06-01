from __future__ import annotations

import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch


PROJECT_ROOT = Path(__file__).resolve().parents[1]
WORKSPACE_ROOT = PROJECT_ROOT.parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.backup.archiver import create_archive  # noqa: E402
from app.backup.discovery import JobDefinition  # noqa: E402
from app.backup.filters import DiscoveredFile  # noqa: E402
from app.core.config import Settings  # noqa: E402
from app.core.encryption import encrypt_file  # noqa: E402
from app.services import recovery as recovery_module  # noqa: E402
from app.services.recovery import RecoveryService  # noqa: E402
from app.services.state import StateStore  # noqa: E402


class _StubUploadClient:
    def __init__(self, encrypted_archive: Path) -> None:
        self.encrypted_archive = encrypted_archive

    def download_latest_snapshot(self, *, edge_id: str, job_name: str, destination: Path) -> dict[str, str]:
        shutil.copyfile(self.encrypted_archive, destination)
        return {"filename": self.encrypted_archive.name, "path": str(destination)}


class RecoveryServiceTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-recovery"
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
        self.quiescer = Mock()
        self.quiescer.prepare.return_value = None

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_recover_latest_overwrites_backed_up_files_and_leaves_extra_files(self) -> None:
        job_root = self.settings.scan_root / "photos"
        job_root.mkdir(parents=True, exist_ok=True)
        (job_root / "notes.txt").write_text("old contents", encoding="utf-8")
        (job_root / "extra.txt").write_text("keep me", encoding="utf-8")

        source_root = self.temp_dir / "source"
        source_root.mkdir(parents=True, exist_ok=True)
        (source_root / "notes.txt").write_text("restored contents", encoding="utf-8")
        (source_root / "nested").mkdir(parents=True, exist_ok=True)
        (source_root / "nested" / "child.txt").write_text("nested restore", encoding="utf-8")

        files = [
            DiscoveredFile(
                source_path=source_root / "notes.txt",
                archive_path="notes.txt",
                size=(source_root / "notes.txt").stat().st_size,
                mtime_ns=(source_root / "notes.txt").stat().st_mtime_ns,
            ),
            DiscoveredFile(
                source_path=source_root / "nested" / "child.txt",
                archive_path="nested/child.txt",
                size=(source_root / "nested" / "child.txt").stat().st_size,
                mtime_ns=(source_root / "nested" / "child.txt").stat().st_mtime_ns,
            ),
        ]

        archive_path = self.temp_dir / "photos__latest.tar.zst"
        create_archive(archive_path, files)
        encrypted_archive = self.temp_dir / "photos__latest.tar.zst.enc"
        key_path = self.temp_dir / "encryption.key"
        key = b"a" * 32
        key_path.write_bytes(key)
        encrypt_file(key, archive_path, encrypted_archive)

        service = RecoveryService(
            settings=self.settings,
            logger=self.logger,
            state_store=self.state_store,
            upload_client=_StubUploadClient(encrypted_archive),
            quiescer=self.quiescer,
        )
        job = JobDefinition(
            root_path=job_root,
            job_name="photos",
            exclude_patterns=[],
            include_hidden=True,
            follow_symlinks=False,
        )

        with patch.object(recovery_module, "encryption_key_path", return_value=key_path):
            result = service.recover_latest(job)

        self.assertEqual(result["status"], "recovered")
        self.assertEqual(result["restored_files"], 2)
        self.assertEqual((job_root / "notes.txt").read_text(encoding="utf-8"), "restored contents")
        self.assertEqual((job_root / "nested" / "child.txt").read_text(encoding="utf-8"), "nested restore")
        self.assertEqual((job_root / "extra.txt").read_text(encoding="utf-8"), "keep me")
        saved_state = self.state_store.get(job.state_key)
        self.assertEqual(saved_state.last_status, "recovered")
        self.assertIsNone(saved_state.last_error_detail)
        self.quiescer.prepare.assert_called_once_with(job)
        self.quiescer.restore.assert_called_once_with(job, None)


if __name__ == "__main__":
    unittest.main()
