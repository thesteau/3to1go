from __future__ import annotations

import shutil
import sys
import unittest
from pathlib import Path
from uuid import uuid4


PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.backup.discovery import UPLOAD_DIR_FILENAME, discover_jobs  # noqa: E402


class _Logger:
    def info(self, *_args, **_kwargs) -> None:
        return None

    def warning(self, *_args, **_kwargs) -> None:
        return None

    def error(self, *_args, **_kwargs) -> None:
        return None


class DiscoveryTests(unittest.TestCase):
    def setUp(self) -> None:
        root = PROJECT_ROOT / ".tmp-test-discovery" / uuid4().hex
        root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = root
        self.scan_root = root / "scan"
        self.scan_root.mkdir(parents=True, exist_ok=True)

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_discovery_is_breadth_first_and_blocks_nested_jobs_under_selected_parent(self) -> None:
        alpha = self.scan_root / "alpha"
        beta = self.scan_root / "beta"
        nested = alpha / "nested"
        for path in (alpha, beta, nested):
            path.mkdir(parents=True, exist_ok=True)

        (alpha / UPLOAD_DIR_FILENAME).write_text("", encoding="utf-8")
        (beta / UPLOAD_DIR_FILENAME).write_text("", encoding="utf-8")
        (nested / UPLOAD_DIR_FILENAME).write_text("", encoding="utf-8")

        jobs = discover_jobs(self.scan_root, max_depth=5, logger=_Logger())

        self.assertEqual([job.root_path for job in jobs], [alpha.resolve(), beta.resolve()])


if __name__ == "__main__":
    unittest.main()
