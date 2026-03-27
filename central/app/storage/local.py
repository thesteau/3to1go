from __future__ import annotations

import os
from pathlib import Path

from app.storage.base import StorageBackend


class LocalFilesystemBackend(StorageBackend):
    def __init__(self, backup_root: Path) -> None:
        self.backup_root = backup_root
        self.backup_root.mkdir(parents=True, exist_ok=True)

    def store(self, namespace: str, filename: str, staged_path: Path) -> dict:
        target_dir = self.backup_root / namespace
        target_dir.mkdir(parents=True, exist_ok=True)

        final_path = target_dir / filename
        os.replace(staged_path, final_path)

        return {
            "namespace": namespace,
            "stored_as": filename,
            "path": str(final_path),
        }

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
                }
            )

        return items

    def delete(self, namespace: str, filename: str) -> None:
        target_path = self.backup_root / namespace / filename
        if target_path.exists():
            target_path.unlink()

    def healthcheck(self) -> bool:
        try:
            self.backup_root.mkdir(parents=True, exist_ok=True)
            probe_path = self.backup_root / ".healthcheck"
            with probe_path.open("w", encoding="utf-8") as handle:
                handle.write("ok")
                handle.flush()
                os.fsync(handle.fileno())
            probe_path.unlink(missing_ok=True)
            return True
        except OSError:
            return False
