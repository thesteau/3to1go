from __future__ import annotations

import sys
import shutil
import unittest
from pathlib import Path
from unittest.mock import patch
from uuid import uuid4

from fastapi.testclient import TestClient


PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.api.app import create_app  # noqa: E402
from app.core.config import Settings  # noqa: E402


class HealthcheckTests(unittest.TestCase):
    def setUp(self) -> None:
        root = PROJECT_ROOT / ".tmp-test-healthcheck" / uuid4().hex
        root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = root
        self.settings = Settings(
            auth_token="secret",
            storage_backend="local",
            backup_root=root / "backups",
            retention_keep_last=3,
            log_level="INFO",
            max_upload_size_mb=16,
            upload_chunk_size_mb=2,
            upload_session_ttl_hours=24,
            upload_cleanup_interval_seconds=60,
            staging_dir=root / "staging",
            http_host="127.0.0.1",
            http_port=8000,
        )
        self.client = TestClient(create_app(settings=self.settings))

    def tearDown(self) -> None:
        self.client.close()
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_health_ready_avoids_directory_walk(self) -> None:
        with patch("app.api.routes_overview._directory_usage", side_effect=AssertionError("directory walk should not run")):
            response = self.client.get("/health/ready")

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.json(), {"status": "ok"})


if __name__ == "__main__":
    unittest.main()
