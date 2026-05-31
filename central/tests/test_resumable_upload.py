from __future__ import annotations

import json
import sys
import shutil
import tempfile
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


class ResumableUploadTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-resumable-upload"
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
            staging_dir=self.temp_dir / "staging",
            http_host="127.0.0.1",
            http_port=6555,
        )
        self.client = TestClient(create_app(settings=self.settings))
        self.headers = {"Authorization": "Bearer secret"}

    def tearDown(self) -> None:
        self.client.close()
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_resumes_upload_and_completes_idempotently(self) -> None:
        archive_bytes = b"hello world"
        archive_sha256 = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
        init_payload = {
            "edge_id": "edge-01",
            "edge_instance_id": "edgeinstance0001",
            "job_name": "photos",
            "fingerprint": "abcdef1234567890",
            "timestamp": "2026-04-05T12:00:00Z",
            "archive_format": "tar.zst",
            "archive_size_bytes": len(archive_bytes),
            "archive_sha256": archive_sha256,
            "idempotency_key": "idem-12345678",
            "encryption_key_fingerprint": "f" * 64,
        }

        init_response = self.client.post("/backup/uploads/initiate", json=init_payload, headers=self.headers)
        self.assertEqual(init_response.status_code, 200, init_response.text)
        upload_id = init_response.json()["upload_id"]

        first_chunk = self.client.put(
            f"/backup/uploads/{upload_id}/chunk?offset=0",
            content=archive_bytes[:6],
            headers=self.headers,
        )
        self.assertEqual(first_chunk.status_code, 200, first_chunk.text)
        self.assertEqual(first_chunk.json()["next_offset"], 6)

        resumed = self.client.post("/backup/uploads/initiate", json=init_payload, headers=self.headers)
        self.assertEqual(resumed.status_code, 200, resumed.text)
        self.assertEqual(resumed.json()["upload_id"], upload_id)
        self.assertEqual(resumed.json()["next_offset"], 6)

        second_chunk = self.client.put(
            f"/backup/uploads/{upload_id}/chunk?offset=6",
            content=archive_bytes[6:],
            headers=self.headers,
        )
        self.assertEqual(second_chunk.status_code, 200, second_chunk.text)
        self.assertEqual(second_chunk.json()["next_offset"], len(archive_bytes))

        finalize = self.client.post(f"/backup/uploads/{upload_id}/finalize", headers=self.headers)
        self.assertEqual(finalize.status_code, 200, finalize.text)

        duplicate = self.client.post("/backup/uploads/initiate", json=init_payload, headers=self.headers)
        self.assertEqual(duplicate.status_code, 200, duplicate.text)
        self.assertEqual(duplicate.json()["status"], "completed")
        self.assertEqual(duplicate.json()["next_offset"], len(archive_bytes))

        stored_files = list((self.settings.backup_root / "edge-01" / "photos").glob("*.tar.zst"))
        self.assertEqual(len(stored_files), 1)

        committed_duplicate_payload = dict(init_payload)
        committed_duplicate_payload["timestamp"] = "2026-04-05T12:10:00Z"
        committed_duplicate_payload["idempotency_key"] = "idem-duplicate-1"
        committed_duplicate = self.client.post(
            "/backup/uploads/initiate",
            json=committed_duplicate_payload,
            headers=self.headers,
        )
        self.assertEqual(committed_duplicate.status_code, 200, committed_duplicate.text)
        self.assertEqual(committed_duplicate.json()["status"], "completed")
        self.assertTrue(committed_duplicate.json()["duplicate"])
        self.assertEqual(committed_duplicate.json()["stored_as"], stored_files[0].name)

        self.assertEqual(stored_files[0].read_bytes(), archive_bytes)

    def test_rejects_over_reserved_capacity(self) -> None:
        ingest_service = self.client.app.state.ingest_service
        payload_one = {
            "edge_id": "edge-01",
            "edge_instance_id": "edgeinstance0001",
            "job_name": "photos",
            "fingerprint": "abcdef1234567890",
            "timestamp": "2026-04-05T12:00:00Z",
            "archive_format": "tar.zst",
            "archive_size_bytes": 6,
            "archive_sha256": "e9c0f8b575cbfcb42ab3b78ecc87efa3b011d9a5d10b09fa4e96f240bf6a82f5",
            "idempotency_key": "idem-a1234567",
            "encryption_key_fingerprint": "a" * 64,
        }
        payload_two = {
            "edge_id": "edge-01",
            "edge_instance_id": "edgeinstance0001",
            "job_name": "videos",
            "fingerprint": "12345678abcdef90",
            "timestamp": "2026-04-05T12:05:00Z",
            "archive_format": "tar.zst",
            "archive_size_bytes": self.settings.max_upload_size_bytes,
            "archive_sha256": "3ec42f5dd09802311d4460e6cdf43d736d35d15eb0b90043956ff0bcae71d6e3",
            "idempotency_key": "idem-b1234567",
            "encryption_key_fingerprint": "a" * 64,
        }

        with patch.object(ingest_service, "_free_space_bytes", return_value=self.settings.max_upload_size_bytes + 5):
            first = self.client.post("/backup/uploads/initiate", json=payload_one, headers=self.headers)
            self.assertEqual(first.status_code, 200, first.text)

            second = self.client.post("/backup/uploads/initiate", json=payload_two, headers=self.headers)
            self.assertEqual(second.status_code, 507, second.text)

    def test_rejects_edge_id_collision_from_different_instance(self) -> None:
        payload = {
            "edge_id": "edge-01",
            "edge_instance_id": "edgeinstance0001",
            "job_name": "photos",
            "fingerprint": "abcdef1234567890",
            "timestamp": "2026-04-05T12:00:00Z",
            "archive_format": "tar.zst",
            "archive_size_bytes": 6,
            "archive_sha256": "e9c0f8b575cbfcb42ab3b78ecc87efa3b011d9a5d10b09fa4e96f240bf6a82f5",
            "idempotency_key": "idem-a1234567",
            "encryption_key_fingerprint": "a" * 64,
        }
        first = self.client.post(
            "/backup/uploads/initiate",
            json=payload,
            headers={**self.headers, "X-Forwarded-For": "192.168.1.120"},
        )
        self.assertEqual(first.status_code, 200, first.text)

        conflicting = dict(payload)
        conflicting["edge_instance_id"] = "edgeinstance9999"
        conflicting["idempotency_key"] = "idem-c1234567"
        conflict = self.client.post(
            "/backup/uploads/initiate",
            json=conflicting,
            headers={**self.headers, "X-Forwarded-For": "192.168.1.121"},
        )
        self.assertEqual(conflict.status_code, 409, conflict.text)
        detail = conflict.json()["detail"]
        self.assertEqual(detail["status"], "edge_id_conflict")
        self.assertEqual(detail["edge_id"], "edge-01")
        self.assertEqual(detail["registered_instance_id"], "edgeinstance0001")
        self.assertEqual(detail["incoming_instance_id"], "edgeinstance9999")
        self.assertEqual(detail["registered_source_address"], "192.168.1.120")
        self.assertEqual(detail["incoming_source_address"], "192.168.1.121")

    def test_overview_includes_edge_registration_metadata(self) -> None:
        archive_bytes = b"hello world"
        payload = {
            "edge_id": "edge-01",
            "edge_instance_id": "edgeinstance0001",
            "job_name": "photos",
            "fingerprint": "abcdef1234567890",
            "timestamp": "2026-04-05T12:00:00Z",
            "archive_format": "tar.zst",
            "archive_size_bytes": len(archive_bytes),
            "archive_sha256": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
            "idempotency_key": "idem-overview1",
            "encryption_key_fingerprint": "c" * 64,
        }

        init_response = self.client.post(
            "/backup/uploads/initiate",
            json=payload,
            headers={**self.headers, "X-Forwarded-For": "192.168.1.120"},
        )
        self.assertEqual(init_response.status_code, 200, init_response.text)
        upload_id = init_response.json()["upload_id"]
        chunk = self.client.put(
            f"/backup/uploads/{upload_id}/chunk?offset=0",
            content=archive_bytes,
            headers=self.headers,
        )
        self.assertEqual(chunk.status_code, 200, chunk.text)
        finalize = self.client.post(f"/backup/uploads/{upload_id}/finalize", headers=self.headers)
        self.assertEqual(finalize.status_code, 200, finalize.text)

        overview = self.client.get("/api/overview")
        self.assertEqual(overview.status_code, 200, overview.text)
        namespace = overview.json()["namespaces"][0]
        self.assertEqual(namespace["edge_id"], "edge-01")
        self.assertEqual(namespace["edge_instance_id"], "edgeinstance0001")
        self.assertEqual(namespace["encryption_key_fingerprint"], "c" * 64)
        self.assertEqual(namespace["last_seen_source"], "192.168.1.120")

    def test_reuses_idempotency_key_after_manual_snapshot_deletion(self) -> None:
        archive_bytes = b"hello world"
        payload = {
            "edge_id": "edge-01",
            "edge_instance_id": "edgeinstance0001",
            "job_name": "photos",
            "fingerprint": "abcdef1234567890",
            "timestamp": "2026-04-05T12:00:00Z",
            "archive_format": "tar.zst",
            "archive_size_bytes": len(archive_bytes),
            "archive_sha256": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
            "idempotency_key": "idem-deleted1",
            "encryption_key_fingerprint": "d" * 64,
        }

        init_response = self.client.post("/backup/uploads/initiate", json=payload, headers=self.headers)
        self.assertEqual(init_response.status_code, 200, init_response.text)
        first_upload_id = init_response.json()["upload_id"]

        chunk = self.client.put(
            f"/backup/uploads/{first_upload_id}/chunk?offset=0",
            content=archive_bytes,
            headers=self.headers,
        )
        self.assertEqual(chunk.status_code, 200, chunk.text)

        finalize = self.client.post(f"/backup/uploads/{first_upload_id}/finalize", headers=self.headers)
        self.assertEqual(finalize.status_code, 200, finalize.text)

        stored_path = self.settings.backup_root / "edge-01" / "photos" / finalize.json()["stored_as"]
        stored_path.unlink()

        restarted = self.client.post("/backup/uploads/initiate", json=payload, headers=self.headers)
        self.assertEqual(restarted.status_code, 200, restarted.text)
        self.assertEqual(restarted.json()["status"], "initiated")
        self.assertEqual(restarted.json()["next_offset"], 0)
        self.assertNotEqual(restarted.json()["upload_id"], first_upload_id)

    def test_delete_snapshot_api_reconciles_committed_index(self) -> None:
        archive_bytes = b"hello world"
        payload = {
            "edge_id": "edge-01",
            "edge_instance_id": "edgeinstance0001",
            "job_name": "photos",
            "fingerprint": "abcdef1234567890",
            "timestamp": "2026-04-05T12:00:00Z",
            "archive_format": "tar.zst",
            "archive_size_bytes": len(archive_bytes),
            "archive_sha256": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
            "idempotency_key": "idem-delete-api1",
            "encryption_key_fingerprint": "e" * 64,
        }

        init_response = self.client.post("/backup/uploads/initiate", json=payload, headers=self.headers)
        self.assertEqual(init_response.status_code, 200, init_response.text)
        upload_id = init_response.json()["upload_id"]

        chunk = self.client.put(
            f"/backup/uploads/{upload_id}/chunk?offset=0",
            content=archive_bytes,
            headers=self.headers,
        )
        self.assertEqual(chunk.status_code, 200, chunk.text)

        finalize = self.client.post(f"/backup/uploads/{upload_id}/finalize", headers=self.headers)
        self.assertEqual(finalize.status_code, 200, finalize.text)
        stored_as = finalize.json()["stored_as"]

        index_path = self.settings.backup_root / ".relay_index" / "edge-01" / "photos" / "committed.json"
        index_entries = json.loads(index_path.read_text(encoding="utf-8"))
        self.assertEqual([entry["stored_as"] for entry in index_entries], [stored_as])

        delete_response = self.client.delete(f"/api/snapshots/edge-01/photos/{stored_as}")
        self.assertEqual(delete_response.status_code, 200, delete_response.text)

        updated_entries = json.loads(index_path.read_text(encoding="utf-8"))
        self.assertEqual(updated_entries, [])

if __name__ == "__main__":
    unittest.main()
