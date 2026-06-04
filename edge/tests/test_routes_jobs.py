from __future__ import annotations

import shutil
import sys
import tempfile
import os
import unittest
from pathlib import Path
from unittest.mock import patch

from fastapi.testclient import TestClient


PROJECT_ROOT = Path(__file__).resolve().parents[1]
WORKSPACE_ROOT = PROJECT_ROOT.parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.api.app import create_app  # noqa: E402
from app.core.config import Settings  # noqa: E402


class JobRoutesTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-job-routes"
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
        self.client = TestClient(
            create_app(
                settings=self.settings,
                start_scheduler=False,
                user_store_path=self.temp_dir / "edge-users.db",
            )
        )
        login = self.client.post(
            "/api/session/login",
            json={"username": "admin", "password": "admin"},
        )
        self.assertEqual(login.status_code, 200, login.text)
        password = self.client.post(
            "/api/session/change-password",
            json={
                "current_password": "admin",
                "new_password": "changed-admin",
                "confirm_new_password": "changed-admin",
            },
        )
        self.assertEqual(password.status_code, 200, password.text)

    def tearDown(self) -> None:
        self.client.close()
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_directories_route_reports_scan_dir_env_value(self) -> None:
        with patch.dict("os.environ", {"SCAN_DIR": "./scan"}):
            response = self.client.get("/api/directories")

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.json()["scan_dir"], "./scan")
        self.assertEqual(response.json()["scan_root"], str(self.settings.scan_root))

    def test_directories_route_defaults_scan_dir_display_to_container_mount(self) -> None:
        with patch.dict("os.environ", {}, clear=False):
            os.environ.pop("SCAN_DIR", None)
            response = self.client.get("/api/directories")

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.json()["scan_dir"], "/scan")
        self.assertEqual(response.json()["scan_root"], str(self.settings.scan_root))

    def test_force_send_route_returns_runner_payload(self) -> None:
        with patch.object(
            self.client.app.state.runner,
            "force_send_job",
            return_value={"status": "started", "job_name": "photos", "manual_retry_cleared": True},
        ) as force_send_job:
            response = self.client.post("/api/jobs/force-send?job_name=photos")

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.json()["status"], "started")
        force_send_job.assert_called_once_with("photos")

    def test_force_send_route_maps_missing_job_to_404(self) -> None:
        with patch.object(
            self.client.app.state.runner,
            "force_send_job",
            side_effect=ValueError("job not found"),
        ):
            response = self.client.post("/api/jobs/force-send?job_name=photos")

        self.assertEqual(response.status_code, 404, response.text)
        self.assertEqual(response.json()["detail"], "job not found")

    def test_recover_latest_route_returns_runner_payload(self) -> None:
        with patch.object(
            self.client.app.state.runner,
            "recover_job",
            return_value={"status": "recovered", "snapshot_filename": "photos__latest.tar.zst", "restored_files": 2},
        ) as recover_job:
            response = self.client.post("/api/jobs/recover-latest?relative_path=photos")

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.json()["status"], "recovered")
        recover_job.assert_called_once_with("photos", fingerprint=None)

    def test_recover_preview_route_returns_runner_payload(self) -> None:
        with patch.object(
            self.client.app.state.runner,
            "preview_recovery",
            return_value={
                "status": "preview",
                "snapshot_filename": "photos__latest.tar.zst",
                "total_files": 2,
                "replace_count": 1,
                "add_count": 1,
                "entries": [{"path": "notes.txt", "action": "replace", "size": 12}],
            },
        ) as preview_recovery:
            response = self.client.get("/api/jobs/recover-preview?relative_path=photos")

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.json()["status"], "preview")
        preview_recovery.assert_called_once_with("photos", fingerprint=None)


if __name__ == "__main__":
    unittest.main()
