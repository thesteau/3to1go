from __future__ import annotations

import os
from dataclasses import asdict
from pathlib import Path
from typing import Any

from app.backup.discovery import (
    UPLOAD_DIR_FILENAME,
    build_job_definition,
    delete_upload_dir,
    job_definition_to_payload,
    read_upload_dir_payload,
    write_upload_dir,
)
from app.core.config import Settings
from app.services.state import StateStore


class DirectoryService:
    def __init__(self, settings: Settings, logger, state_store: StateStore) -> None:
        self.settings = settings
        self.logger = logger
        self.state_store = state_store

    def list_directories(self) -> list[dict[str, Any]]:
        scan_root = self.settings.scan_root.resolve()
        entries: list[dict[str, Any]] = []

        def walk(directory: Path, depth: int, blocked_by: str | None) -> None:
            if depth > self.settings.max_depth:
                return

            relative_path = "." if directory == scan_root else directory.relative_to(scan_root).as_posix()
            marker_path = directory / UPLOAD_DIR_FILENAME
            has_marker = marker_path.is_file()
            config_error: str | None = None
            config: dict[str, Any] | None = None

            if has_marker:
                config, config_error = self._read_directory_config(directory)

            state = self.state_store.get(str(directory.resolve()))
            entries.append(
                {
                    "relative_path": relative_path,
                    "absolute_path": str(directory),
                    "selected": has_marker,
                    "blocked_by_parent": blocked_by,
                    "config": config,
                    "config_error": config_error,
                    "state": asdict(state),
                }
            )

            try:
                children = sorted(
                    [Path(entry.path) for entry in os.scandir(directory) if entry.is_dir(follow_symlinks=False)],
                    key=lambda item: item.name.lower(),
                )
            except (FileNotFoundError, NotADirectoryError, PermissionError, OSError):
                return

            child_blocked_by = relative_path if has_marker else blocked_by
            for child in children:
                walk(child, depth + 1, child_blocked_by)

        walk(scan_root, 0, None)
        return entries

    def save_job(self, relative_path: str, payload: dict[str, Any]) -> dict[str, Any]:
        directory = self.resolve_directory(relative_path)
        blocking_ancestor = self.find_blocking_ancestor(directory)
        if blocking_ancestor is not None and not (directory / UPLOAD_DIR_FILENAME).exists():
            raise ValueError(f"directory is nested under existing job {blocking_ancestor}")

        write_upload_dir(directory, payload)
        job = build_job_definition(directory, read_upload_dir_payload(directory / UPLOAD_DIR_FILENAME))
        self.logger.info("ui_job_saved path=%s job_name=%s", directory, job.job_name)
        return self.serialize_directory(directory)

    def delete_job(self, relative_path: str) -> None:
        directory = self.resolve_directory(relative_path)
        delete_upload_dir(directory)
        self.state_store.delete(str(directory.resolve()))
        self.logger.info("ui_job_deleted path=%s", directory)

    def serialize_directory(self, directory: Path) -> dict[str, Any]:
        marker_path = directory / UPLOAD_DIR_FILENAME
        config: dict[str, Any] | None = None
        config_error: str | None = None
        if marker_path.exists():
            config, config_error = self._read_directory_config(directory)

        state = self.state_store.get(str(directory.resolve()))
        relative_path = "." if directory.resolve() == self.settings.scan_root.resolve() else directory.resolve().relative_to(self.settings.scan_root.resolve()).as_posix()
        return {
            "relative_path": relative_path,
            "absolute_path": str(directory.resolve()),
            "selected": marker_path.exists(),
            "blocked_by_parent": self.find_blocking_ancestor(directory),
            "config": config,
            "config_error": config_error,
            "state": asdict(state),
        }

    def resolve_directory(self, relative_path: str) -> Path:
        candidate = (self.settings.scan_root / relative_path).resolve() if relative_path != "." else self.settings.scan_root.resolve()
        try:
            candidate.relative_to(self.settings.scan_root.resolve())
        except ValueError as exc:
            raise ValueError("path must remain within scan root") from exc
        if not candidate.exists() or not candidate.is_dir():
            raise ValueError("directory not found")
        return candidate

    def find_blocking_ancestor(self, directory: Path) -> str | None:
        resolved_root = self.settings.scan_root.resolve()
        resolved_directory = directory.resolve()
        if resolved_directory == resolved_root:
            return None

        current = resolved_directory.parent
        while True:
            if current == resolved_root.parent:
                return None
            if (current / UPLOAD_DIR_FILENAME).exists() and current != resolved_directory:
                return "." if current == resolved_root else current.relative_to(resolved_root).as_posix()
            if current == resolved_root:
                return None
            current = current.parent

    def _read_directory_config(self, directory: Path) -> tuple[dict[str, Any] | None, str | None]:
        marker_path = directory / UPLOAD_DIR_FILENAME
        try:
            payload = read_upload_dir_payload(marker_path)
            return job_definition_to_payload(build_job_definition(directory, payload)), None
        except (ValueError, OSError) as exc:
            return None, str(exc)
