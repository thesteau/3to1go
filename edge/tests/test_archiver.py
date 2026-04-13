from __future__ import annotations

import io
import shutil
import sys
import tarfile
import unittest
from dataclasses import dataclass
from pathlib import Path
from uuid import uuid4

import zstandard


PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.backup.archiver import create_archive  # noqa: E402


@dataclass
class _DiscoveredFile:
    source_path: Path
    archive_path: str
    size: int
    mtime_ns: int


class ArchiverTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = PROJECT_ROOT / ".tmp-test-archiver" / uuid4().hex
        self.temp_dir.mkdir(parents=True, exist_ok=True)

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_create_archive_writes_zstd_tar_without_closing_raw_file_early(self) -> None:
        source = self.temp_dir / "hello.txt"
        source.write_text("hello from archiver test", encoding="utf-8")
        archive_path = self.temp_dir / "archive.tar.zst"

        create_archive(
            archive_path=archive_path,
            files=[
                _DiscoveredFile(
                    source_path=source,
                    archive_path="hello.txt",
                    size=source.stat().st_size,
                    mtime_ns=source.stat().st_mtime_ns,
                )
            ],
        )

        self.assertTrue(archive_path.exists())
        self.assertGreater(archive_path.stat().st_size, 0)

        decompressor = zstandard.ZstdDecompressor()
        with archive_path.open("rb") as handle:
            tar_bytes = decompressor.stream_reader(handle).read()

        with tarfile.open(fileobj=io.BytesIO(tar_bytes), mode="r:") as tar_handle:
            member = tar_handle.getmember("hello.txt")
            extracted = tar_handle.extractfile(member)
            self.assertIsNotNone(extracted)
            self.assertEqual(extracted.read(), b"hello from archiver test")


if __name__ == "__main__":
    unittest.main()
