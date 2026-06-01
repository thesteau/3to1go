from __future__ import annotations

import json
import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from urllib import error

from fastapi.testclient import TestClient


PROJECT_ROOT = Path(__file__).resolve().parents[1]
WORKSPACE_ROOT = PROJECT_ROOT.parent
for module_name in [name for name in sys.modules if name == "app" or name.startswith("app.")]:
    sys.modules.pop(module_name)
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.api.app import create_app  # noqa: E402
from app.core.config import Settings  # noqa: E402


class _UrlOpenResponse:
    def __init__(self, status: int, body: dict) -> None:
        self.status = status
        self._payload = json.dumps(body).encode("utf-8")

    def read(self) -> bytes:
        return self._payload

    def __enter__(self) -> _UrlOpenResponse:
        return self

    def __exit__(self, exc_type, exc, tb) -> bool:
        return False


class EdgeControlsTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-edge-controls"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))
        self.settings = Settings(
            auth_token="secret",
            storage_backend="local",
            backup_root=self.temp_dir / "backups",
            retention_keep_last=3,
            log_level="INFO",
            max_upload_size_mb=16,
            upload_chunk_size_mb=2,
            upload_session_ttl_hours=24,
            upload_cleanup_interval_seconds=60,
            ntfy_url="",
            ntfy_topic="",
            ntfy_message_template="",
            ntfy_match_edge_id="",
            ntfy_match_edge_instance_id="",
            ntfy_match_source="",
            hook_pre_command="",
            hook_post_command="",
            staging_dir=self.temp_dir / "staging",
            http_host="127.0.0.1",
            http_port=6555,
        )
        self.client = TestClient(create_app(settings=self.settings))
        self.client.app.state.snapshot_index.upsert_edge_registration(
            {
                "edge_id": "edge-01",
                "edge_instance_id": "edgeinstance0001",
                "advertised_url": "http://edge-one.example.com:6556",
                "encryption_key_fingerprint": "a" * 64,
                "first_seen_at": "2026-05-31T00:00:00Z",
                "last_seen_at": "2026-05-31T00:00:00Z",
                "last_seen_source": "192.168.1.120",
            }
        )

    def tearDown(self) -> None:
        self.client.close()
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_force_send_proxy_forwards_request_to_registered_edge(self) -> None:
        with patch(
            "app.api.routes_overview.request.urlopen",
            return_value=_UrlOpenResponse(200, {"status": "started", "job_name": "photos"}),
        ) as urlopen:
            response = self.client.post("/api/edges/edge-01/edgeinstance0001/jobs/photos/force-send")

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.json()["status"], "started")
        req = urlopen.call_args.args[0]
        self.assertEqual(
            req.full_url,
            "http://edge-one.example.com:6556/api/jobs/force-send?job_name=photos",
        )

    def test_force_send_proxy_rejects_missing_advertised_url(self) -> None:
        self.client.app.state.snapshot_index.upsert_edge_registration(
            {
                "edge_id": "edge-02",
                "edge_instance_id": "edgeinstance0002",
                "advertised_url": "",
                "encryption_key_fingerprint": "b" * 64,
                "first_seen_at": "2026-05-31T00:00:00Z",
                "last_seen_at": "2026-05-31T00:00:00Z",
                "last_seen_source": "192.168.1.121",
            }
        )

        response = self.client.post("/api/edges/edge-02/edgeinstance0002/jobs/photos/force-send")

        self.assertEqual(response.status_code, 400, response.text)
        self.assertEqual(response.json()["detail"], "edge does not advertise a reachable URL")

    def test_force_send_proxy_maps_edge_network_failure_to_502(self) -> None:
        with patch(
            "app.api.routes_overview.request.urlopen",
            side_effect=error.URLError("connection refused"),
        ):
            response = self.client.post("/api/edges/edge-01/edgeinstance0001/jobs/photos/force-send")

        self.assertEqual(response.status_code, 502, response.text)
        self.assertIn("connection refused", response.json()["detail"])

    def test_recovery_download_returns_latest_snapshot_for_namespace(self) -> None:
        namespace_dir = self.settings.backup_root / "edge-01" / "edgeinstance0001" / "photos"
        namespace_dir.mkdir(parents=True, exist_ok=True)
        older = namespace_dir / "photos__2026-05-30T00-00-00Z__aaaa1111.tar.zst"
        newer = namespace_dir / "photos__2026-05-31T00-00-00Z__bbbb2222.tar.zst"
        older.write_bytes(b"older")
        newer.write_bytes(b"newer")
        os.utime(older, (100, 100))
        os.utime(newer, (200, 200))

        response = self.client.get(
            "/backup/recovery/edge-01/edgeinstance0001/photos/latest",
            headers={"Authorization": "Bearer secret"},
        )

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.content, b"newer")
        self.assertEqual(response.headers["x-relay-snapshot-filename"], newer.name)


if __name__ == "__main__":
    unittest.main()
