from __future__ import annotations

import json
import shutil
import sqlite3
import tempfile
import threading
from pathlib import Path
from typing import Any

from app.core.config import (
    Settings,
    app_database_path,
    build_settings,
    encryption_key_path,
    hook_scripts_dir,
    installation_id_path,
    legacy_encryption_key_path,
    legacy_hook_scripts_dir,
    legacy_installation_id_path,
    legacy_spool_dir,
    legacy_state_dir,
    legacy_settings_storage_path,
    settings_storage_path,
    settings_to_payload,
)


class SettingsStore:
    def __init__(self, path: Path | None = None) -> None:
        self.path = path
        self.sqlite_path = app_database_path()
        self._lock = threading.RLock()
        if self.path is not None:
            self.path.parent.mkdir(parents=True, exist_ok=True)
        else:
            self._ensure_schema()

    def snapshot(self, settings: Settings) -> dict[str, Any]:
        with self._lock:
            return settings_to_payload(settings)

    def save(self, payload: dict[str, Any]) -> Settings:
        settings = build_settings(payload)
        serialized = settings_to_payload(settings)
        with self._lock:
            if self.path is not None:
                self._save_json(self.path, serialized)
            else:
                self._save_sqlite(serialized)
        return settings

    def migration_status(self) -> dict[str, Any]:
        legacy_json_paths = [path for path in (settings_storage_path(), legacy_settings_storage_path()) if path.exists()]
        legacy_hook_files = _legacy_only_files(legacy_hook_scripts_dir(), hook_scripts_dir())
        legacy_files = [path for path in (legacy_encryption_key_path(), legacy_installation_id_path()) if path.exists()]
        legacy_state_files = _list_files(legacy_state_dir())
        legacy_spool_files = _list_files(legacy_spool_dir())
        settings_in_database = self._settings_exists()
        needed = (
            bool(legacy_json_paths)
            or bool(legacy_hook_files)
            or bool(legacy_files)
            or bool(legacy_state_files)
            or bool(legacy_spool_files)
        )
        return {
            "needed": needed,
            "settings_in_database": settings_in_database,
            "legacy_settings_files": [str(path) for path in legacy_json_paths],
            "legacy_hook_dir": str(legacy_hook_scripts_dir()),
            "legacy_hook_files": len(legacy_hook_files),
            "legacy_identity_files": [str(path) for path in legacy_files],
            "legacy_state_files": len(legacy_state_files),
            "legacy_spool_files": len(legacy_spool_files),
        }

    def run_migration(self) -> dict[str, Any]:
        with self._lock:
            migrated_settings = False
            deleted_settings_files: list[str] = []
            moved_files: list[str] = []

            if not self._settings_exists():
                payload = self._load_legacy_payload()
                if payload:
                    self.save(payload)
                    migrated_settings = True

            for path in (settings_storage_path(), legacy_settings_storage_path()):
                if path.exists():
                    try:
                        path.unlink()
                        deleted_settings_files.append(str(path))
                    except OSError:
                        pass

            for source, target in (
                (legacy_encryption_key_path(), encryption_key_path()),
                (legacy_installation_id_path(), installation_id_path()),
            ):
                if source.exists() and not target.exists():
                    target.parent.mkdir(parents=True, exist_ok=True)
                    try:
                        shutil.move(str(source), str(target))
                        moved_files.append(str(target))
                    except OSError:
                        pass

            source_dir = legacy_hook_scripts_dir()
            target_dir = hook_scripts_dir()
            if source_dir.exists():
                target_dir.mkdir(parents=True, exist_ok=True)
                for source in _list_files(source_dir):
                    target = _unique_target(target_dir / source.name)
                    if _same_file(source, target_dir / source.name):
                        continue
                    try:
                        shutil.move(str(source), str(target))
                        moved_files.append(str(target))
                    except OSError:
                        pass
                _remove_empty_parents(source_dir, stop_at=source_dir.parent.parent)

            for source_dir, target_dir in (
                (legacy_state_dir(), build_settings({}).state_dir),
                (legacy_spool_dir(), build_settings({}).spool_dir),
            ):
                if source_dir.exists() and source_dir.is_dir():
                    target_dir.mkdir(parents=True, exist_ok=True)
                    for source in _list_files(source_dir):
                        target = _unique_target(target_dir / source.name)
                        if _same_file(source, target_dir / source.name):
                            continue
                        try:
                            shutil.move(str(source), str(target))
                            moved_files.append(str(target))
                        except OSError:
                            pass
                    _remove_empty_parents(source_dir, stop_at=source_dir.parent)

            return {
                "status": "ok",
                "migrated_settings": migrated_settings,
                "deleted_settings_files": deleted_settings_files,
                "moved_files": moved_files,
                "migration": self.migration_status(),
            }

    def _ensure_schema(self) -> None:
        self.sqlite_path.parent.mkdir(parents=True, exist_ok=True)
        with self._connect_sqlite() as conn:
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS app_settings (
                    key TEXT PRIMARY KEY,
                    payload TEXT NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                )
                """
            )

    def _settings_exists(self) -> bool:
        if self.path is not None:
            return self.path.exists()
        try:
            with self._connect_sqlite() as conn:
                row = conn.execute("SELECT 1 FROM app_settings WHERE key = ?", ("settings",)).fetchone()
            return row is not None
        except sqlite3.Error:
            return False

    def _load_legacy_payload(self) -> dict[str, Any]:
        for path in (settings_storage_path(), legacy_settings_storage_path()):
            if not path.exists():
                continue
            try:
                data = json.loads(path.read_text(encoding="utf-8"))
            except (json.JSONDecodeError, OSError):
                continue
            if isinstance(data, dict):
                return data
        return {}

    def _save_json(self, path: Path, serialized: dict[str, Any]) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, delete=False, suffix=".tmp") as handle:
            json.dump(serialized, handle, indent=2, sort_keys=True)
            handle.flush()
            temp_path = Path(handle.name)
        temp_path.replace(path)

    def _save_sqlite(self, serialized: dict[str, Any]) -> None:
        with self._connect_sqlite() as conn:
            conn.execute(
                """
                INSERT INTO app_settings (key, payload, updated_at)
                VALUES (?, ?, CURRENT_TIMESTAMP)
                ON CONFLICT(key)
                DO UPDATE SET payload = excluded.payload, updated_at = CURRENT_TIMESTAMP
                """,
                ("settings", json.dumps(serialized, sort_keys=True)),
            )

    def _connect_sqlite(self) -> sqlite3.Connection:
        return sqlite3.connect(self.sqlite_path)


def _list_files(path: Path) -> list[Path]:
    if not path.exists() or not path.is_dir():
        return []
    return [entry for entry in path.iterdir() if entry.is_file()]


def _legacy_only_files(source_dir: Path, target_dir: Path) -> list[Path]:
    return [
        source
        for source in _list_files(source_dir)
        if not _same_file(source, target_dir / source.name)
    ]


def _unique_target(path: Path) -> Path:
    if not path.exists():
        return path
    stem = path.stem
    suffix = path.suffix
    for index in range(1, 1000):
        candidate = path.with_name(f"{stem}-{index}{suffix}")
        if not candidate.exists():
            return candidate
    return path.with_name(f"{stem}-{id(path)}{suffix}")


def _same_file(left: Path, right: Path) -> bool:
    try:
        return left.exists() and right.exists() and left.samefile(right)
    except OSError:
        return False


def _remove_empty_parents(path: Path, stop_at: Path) -> None:
    current = path
    while current != stop_at and current.exists():
        try:
            current.rmdir()
        except OSError:
            return
        current = current.parent
