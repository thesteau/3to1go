from __future__ import annotations

import errno
import shutil
import sys
import unittest
from pathlib import Path
from unittest.mock import patch
from uuid import uuid4


PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.storage.local import LocalFilesystemBackend  # noqa: E402


class LocalFilesystemBackendTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = PROJECT_ROOT / ".tmp-test-local-storage" / uuid4().hex
        self.temp_dir.mkdir(parents=True, exist_ok=True)

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_store_falls_back_when_replace_hits_cross_device_error(self) -> None:
        backup_root = self.temp_dir / "backups"
        staged_path = self.temp_dir / "staging" / "archive.part"
        staged_path.parent.mkdir(parents=True, exist_ok=True)
        staged_path.write_bytes(b"archive bytes")

        backend = LocalFilesystemBackend(backup_root)

        import app.storage.local as local_module

        original_replace = local_module.os.replace

        def replace_with_exdev_once(src, dst):
            if Path(src) == staged_path:
                raise OSError(errno.EXDEV, "Invalid cross-device link")
            return original_replace(src, dst)

        with patch.object(local_module.os, "replace", side_effect=replace_with_exdev_once):
            result = backend.store("edge-01/photos", "archive.tar.zst", staged_path)

        final_path = backup_root / "edge-01" / "photos" / "archive.tar.zst"
        self.assertEqual(result["path"], str(final_path))
        self.assertTrue(final_path.exists())
        self.assertEqual(final_path.read_bytes(), b"archive bytes")
        self.assertFalse(staged_path.exists())


if __name__ == "__main__":
    unittest.main()
