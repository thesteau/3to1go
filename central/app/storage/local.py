from __future__ import annotations

import errno
import hashlib
import os
import shutil
import tempfile
import uuid
from pathlib import Path

from app.storage.base import StorageBackend


class LocalFilesystemBackend(StorageBackend):
    def __init__(self, backup_root: Path, probe_root: Path | None = None) -> None:
        self.backup_root = backup_root
        self.probe_root = probe_root or (Path(tempfile.gettempdir()) / "relay-central" / "healthchecks")
        probe_name = hashlib.sha256(str(self.backup_root).encode("utf-8")).hexdigest()[:16]
        self.probe_path = self.probe_root / f"{probe_name}.healthcheck"

    def store(self, namespace: str, filename: str, staged_path: Path) -> dict:
        target_dir = self.backup_root / namespace
        target_dir.mkdir(parents=True, exist_ok=True)

        final_path = target_dir / filename
        try:
            os.replace(staged_path, final_path)
        except OSError as exc:
            if exc.errno != errno.EXDEV:
                raise
            self._store_across_filesystems(staged_path, final_path)

        return {
            "namespace": namespace,
            "stored_as": filename,
            "path": str(final_path),
        }

    def _store_across_filesystems(self, staged_path: Path, final_path: Path) -> None:
        temp_path = final_path.with_name(f".{final_path.name}.{uuid.uuid4().hex}.tmp")
        try:
            with staged_path.open("rb") as source_handle, temp_path.open("wb") as temp_handle:
                shutil.copyfileobj(source_handle, temp_handle)
                temp_handle.flush()
                os.fsync(temp_handle.fileno())
            os.replace(temp_path, final_path)
            staged_path.unlink()
        finally:
            temp_path.unlink(missing_ok=True)

    def list(self, namespace: str) -> list[dict]:
        target_dir = self.backup_root / namespace
        if not target_dir.exists():
            return []

        items: list[dict] = []
        for entry in target_dir.iterdir():
            if not entry.is_file():
                continue
            stat_result = entry.stat()
            items.append(
                {
                    "filename": entry.name,
                    "path": str(entry),
                    "mtime": stat_result.st_mtime,
                    "size_bytes": stat_result.st_size,
                }
            )

        return items

    def delete(self, namespace: str, filename: str) -> None:
        target_path = self.backup_root / namespace / filename
        if target_path.exists():
            target_path.unlink()

    def healthcheck(self) -> bool:
        try:
            if not self.backup_root.exists() or not self.backup_root.is_dir():
                return False
            if not os.access(self.backup_root, os.W_OK | os.X_OK):
                return False
            self.probe_path.parent.mkdir(parents=True, exist_ok=True)
            mode = "r+b" if self.probe_path.exists() else "w+b"
            with self.probe_path.open(mode) as handle:
                handle.seek(0)
                handle.write(b"ok\n")
                handle.truncate()
                handle.flush()
                os.fsync(handle.fileno())
            return True
        except OSError:
            return False
