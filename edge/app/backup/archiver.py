from __future__ import annotations

import os
import tarfile
from datetime import datetime, timezone
from pathlib import Path

import zstandard

from app.backup.filters import DiscoveredFile


def timestamp_for_filename(timestamp: datetime) -> str:
    return timestamp.astimezone(timezone.utc).strftime("%Y-%m-%dT%H-%M-%SZ")


def timestamp_for_api(timestamp: datetime) -> str:
    return timestamp.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def build_archive_name(job_name: str, timestamp: datetime, fingerprint: str) -> str:
    return f"{job_name}__{timestamp_for_filename(timestamp)}__{fingerprint[:8]}.tar.zst"


def create_archive(archive_path: Path, files: list[DiscoveredFile]) -> None:
    archive_path.parent.mkdir(parents=True, exist_ok=True)
    compressor = zstandard.ZstdCompressor(level=3)

    with archive_path.open("wb") as raw_handle:
        # Keep the underlying file handle open so we can flush and fsync it
        # after the streaming tar+zstd write completes.
        with compressor.stream_writer(raw_handle, closefd=False) as compressed_handle:
            with tarfile.open(mode="w|", fileobj=compressed_handle, format=tarfile.PAX_FORMAT) as tar_handle:
                for file in sorted(files, key=lambda item: item.archive_path):
                    tar_info = tarfile.TarInfo(name=file.archive_path)
                    tar_info.size = file.size
                    tar_info.mtime = file.mtime_ns / 1_000_000_000
                    tar_info.mode = 0o644
                    tar_info.uid = 0
                    tar_info.gid = 0
                    tar_info.uname = ""
                    tar_info.gname = ""

                    with file.source_path.open("rb") as source_handle:
                        tar_handle.addfile(tar_info, source_handle)

        raw_handle.flush()
        os.fsync(raw_handle.fileno())
