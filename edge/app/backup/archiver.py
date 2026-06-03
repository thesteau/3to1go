from __future__ import annotations

import os
import tarfile
import tempfile
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


def _archive_destination(target_root: Path, member_name: str) -> Path:
    destination = (target_root / member_name).resolve()
    try:
        destination.relative_to(target_root)
    except ValueError as exc:
        raise ValueError(f"invalid archive entry: {member_name}") from exc
    return destination


def list_archive_entries(archive_path: Path, target_root: Path) -> dict[str, object]:
    target_root = target_root.resolve()
    entries: list[dict[str, object]] = []
    replace_count = 0
    add_count = 0
    decompressor = zstandard.ZstdDecompressor()

    with archive_path.open("rb") as raw_handle:
        with decompressor.stream_reader(raw_handle) as decompressed_handle:
            with tarfile.open(mode="r|", fileobj=decompressed_handle) as tar_handle:
                for member in tar_handle:
                    if member.isdir():
                        continue
                    if not member.isfile():
                        raise ValueError(f"unsupported archive entry: {member.name}")

                    destination = _archive_destination(target_root, member.name)
                    action = "replace" if destination.exists() else "add"
                    if action == "replace":
                        replace_count += 1
                    else:
                        add_count += 1
                    entries.append(
                        {
                            "path": member.name,
                            "size": member.size,
                            "mtime": member.mtime,
                            "action": action,
                        }
                    )

    return {
        "entries": entries,
        "total_files": len(entries),
        "replace_count": replace_count,
        "add_count": add_count,
    }


def extract_archive(archive_path: Path, target_root: Path) -> int:
    target_root = target_root.resolve()
    extracted_count = 0
    decompressor = zstandard.ZstdDecompressor()

    with archive_path.open("rb") as raw_handle:
        with decompressor.stream_reader(raw_handle) as decompressed_handle:
            with tarfile.open(mode="r|", fileobj=decompressed_handle) as tar_handle:
                for member in tar_handle:
                    if member.isdir():
                        continue
                    if not member.isfile():
                        raise ValueError(f"unsupported archive entry: {member.name}")

                    destination = _archive_destination(target_root, member.name)
                    source_handle = tar_handle.extractfile(member)
                    if source_handle is None:
                        raise ValueError(f"unable to extract archive entry: {member.name}")

                    destination.parent.mkdir(parents=True, exist_ok=True)
                    with tempfile.NamedTemporaryFile(
                        mode="wb",
                        dir=destination.parent,
                        delete=False,
                        suffix=".restore.tmp",
                    ) as temp_handle:
                        temp_path = Path(temp_handle.name)
                        try:
                            while True:
                                chunk = source_handle.read(1024 * 1024)
                                if not chunk:
                                    break
                                temp_handle.write(chunk)
                            temp_handle.flush()
                            os.fsync(temp_handle.fileno())
                        finally:
                            source_handle.close()

                    try:
                        os.replace(temp_path, destination)
                        os.utime(destination, (member.mtime, member.mtime))
                    finally:
                        temp_path.unlink(missing_ok=True)
                    extracted_count += 1

    return extracted_count
