from __future__ import annotations

import fnmatch
import os
import stat
from dataclasses import dataclass
from pathlib import Path, PurePosixPath

from app.backup.discovery import JobDefinition, UPLOAD_DIR_FILENAME


@dataclass(slots=True)
class DiscoveredFile:
    source_path: Path
    archive_path: str
    size: int
    mtime_ns: int


def build_file_list(job: JobDefinition, logger) -> list[DiscoveredFile]:
    files: list[DiscoveredFile] = []
    stack = [job.root_path]
    visited_dirs: set[Path] = set()

    while stack:
        current_dir = stack.pop()
        visit_key = current_dir.resolve(strict=False) if job.follow_symlinks else current_dir
        if visit_key in visited_dirs:
            continue
        visited_dirs.add(visit_key)

        try:
            entries = list(os.scandir(current_dir))
        except (FileNotFoundError, NotADirectoryError, PermissionError, OSError) as exc:
            logger.warning("skipped_missing path=%s detail=%s", current_dir, exc)
            continue

        for entry in entries:
            relative_path = Path(entry.path).relative_to(job.root_path).as_posix()
            if relative_path == UPLOAD_DIR_FILENAME:
                continue
            if not job.include_hidden and _contains_hidden(relative_path):
                continue
            if _matches_exclude(relative_path, job.exclude_patterns):
                continue

            try:
                if entry.is_dir(follow_symlinks=job.follow_symlinks):
                    stack.append(Path(entry.path))
                    continue
                stat_result = entry.stat(follow_symlinks=job.follow_symlinks)
            except (FileNotFoundError, PermissionError, OSError) as exc:
                logger.warning("skipped_missing path=%s detail=%s", entry.path, exc)
                continue

            if not stat.S_ISREG(stat_result.st_mode):
                continue

            files.append(
                DiscoveredFile(
                    source_path=Path(entry.path),
                    archive_path=relative_path,
                    size=stat_result.st_size,
                    mtime_ns=stat_result.st_mtime_ns,
                )
            )

    files.sort(key=lambda item: item.archive_path)
    return files


def _contains_hidden(relative_path: str) -> bool:
    return any(part.startswith(".") for part in PurePosixPath(relative_path).parts)


def _matches_exclude(relative_path: str, patterns: list[str]) -> bool:
    basename = PurePosixPath(relative_path).name

    for pattern in patterns:
        normalized = pattern.strip()
        if not normalized:
            continue

        if normalized.endswith("/"):
            prefix = normalized.rstrip("/")
            if (
                relative_path == prefix
                or relative_path.startswith(f"{prefix}/")
                or f"/{prefix}/" in relative_path
            ):
                return True
            continue

        if "/" in normalized:
            if fnmatch.fnmatch(relative_path, normalized):
                return True
            continue

        if fnmatch.fnmatch(relative_path, normalized) or fnmatch.fnmatch(basename, normalized):
            return True

    return False
