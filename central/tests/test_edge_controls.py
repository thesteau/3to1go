from __future__ import annotations

import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from fastapi.testclient import TestClient


PROJECT_ROOT = Path(__file__).resolve().parents[1]
WORKSPACE_ROOT = PROJECT_ROOT.parent
for module_name in [name for name in sys.modules if name == "app" or name.startswith("app.")]:
    sys.modules.pop(module_name)
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.api.app import create_app  # noqa: E402
from app.core.config import Settings  # noqa: E402
from app.core.signing import load_or_create_issuer_keypair, mint_credential, public_key_to_bytes  # noqa: E402


class EdgeControlsTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-edge-controls"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))
        key_path = self.temp_dir / "issuer.key"
        private_key, public_key = load_or_create_issuer_keypair(key_path)
        self.credential = mint_credential(private_key)
        self.settings = Settings(
            issuer_key_path=key_path,
            issuer_public_key_bytes=public_key_to_bytes(public_key),
            revoked_credentials=frozenset(),
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
                "encryption_key_fingerprint": "a" * 64,
                "first_seen_at": "2026-05-31T00:00:00Z",
                "last_seen_at": "2026-05-31T00:00:00Z",
            }
        )

    def tearDown(self) -> None:
        self.client.close()
        shutil.rmtree(self.temp_dir, ignore_errors=True)

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
            headers={"Authorization": f"Bearer {self.credential}"},
        )

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.content, b"newer")
        self.assertEqual(response.headers["x-relay-snapshot-filename"], newer.name)

    def test_delete_instance_removes_registration_from_overview(self) -> None:
        instance_dir = self.settings.backup_root / "edge-01" / "edgeinstance0001"
        instance_dir.mkdir(parents=True, exist_ok=True)

        before = self.client.get("/api/overview")
        self.assertEqual(before.status_code, 200, before.text)
        self.assertEqual(before.json()["edges"][0]["instances"][0]["edge_instance_id"], "edgeinstance0001")

        response = self.client.delete("/api/instances/edge-01/edgeinstance0001")
        self.assertEqual(response.status_code, 200, response.text)

        after = self.client.get("/api/overview")
        self.assertEqual(after.status_code, 200, after.text)
        self.assertEqual(after.json()["edges"], [])
        self.assertIsNone(self.client.app.state.snapshot_index.get_edge_registration("edge-01", "edgeinstance0001"))

    def test_delete_instance_fails_when_files_or_index_entries_remain(self) -> None:
        namespace = "edge-01/edgeinstance0001/photos"
        namespace_dir = self.settings.backup_root / namespace
        namespace_dir.mkdir(parents=True, exist_ok=True)
        snapshot = namespace_dir / "photos__2026-05-31T00-00-00Z__aaaa1111.tar.zst"
        snapshot.write_bytes(b"still here")
        self.client.app.state.snapshot_index.upsert_snapshot(
            namespace,
            {
                "stored_as": snapshot.name,
                "archive_sha256": "a" * 64,
                "fingerprint": "aaaa1111",
                "timestamp": "2026-05-31T00:00:00Z",
                "size_bytes": snapshot.stat().st_size,
                "mtime": snapshot.stat().st_mtime,
            },
        )

        with patch("app.api.routes_overview.shutil.rmtree"):
            response = self.client.delete("/api/instances/edge-01/edgeinstance0001")

        self.assertEqual(response.status_code, 409, response.text)
        self.assertEqual(
            response.json()["detail"],
            {
                "message": "instance still has backup files or index entries",
                "cleanup_available": False,
            },
        )
        self.assertTrue(snapshot.exists())
        self.assertIsNotNone(self.client.app.state.snapshot_index.get_edge_registration("edge-01", "edgeinstance0001"))
        self.assertTrue(self.client.app.state.snapshot_index.list_namespace_entries(namespace))

    def test_delete_missing_instance_requires_cleanup_confirmation(self) -> None:
        response = self.client.delete("/api/instances/edge-01/edgeinstance0001")
        self.assertEqual(response.status_code, 409, response.text)
        self.assertEqual(
            response.json()["detail"],
            {
                "message": "instance files not found",
                "cleanup_available": True,
            },
        )
        self.assertIsNotNone(self.client.app.state.snapshot_index.get_edge_registration("edge-01", "edgeinstance0001"))

        cleanup = self.client.delete("/api/instances/edge-01/edgeinstance0001?cleanup_missing=true")
        self.assertEqual(cleanup.status_code, 200, cleanup.text)
        self.assertEqual(cleanup.json()["status"], "cleaned")
        self.assertIsNone(self.client.app.state.snapshot_index.get_edge_registration("edge-01", "edgeinstance0001"))

        overview = self.client.get("/api/overview")
        self.assertEqual(overview.status_code, 200, overview.text)
        self.assertEqual(overview.json()["edges"], [])


if __name__ == "__main__":
    unittest.main()
