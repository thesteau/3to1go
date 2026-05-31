from __future__ import annotations

import json
import tempfile
from pathlib import Path
from typing import Any

from app.index.base import SnapshotIndexBackend


class FileSnapshotIndexBackend(SnapshotIndexBackend):
    backend_name = "file"

    def __init__(self, backup_root: Path) -> None:
        self.backup_root = backup_root
        self.index_root = backup_root / ".relay_index"
        self.registry_root = backup_root / ".relay_registry" / "edges"
        self.index_root.mkdir(parents=True, exist_ok=True)
        self.registry_root.mkdir(parents=True, exist_ok=True)

    def find_duplicate(self, namespace: str, archive_sha256: str) -> dict[str, Any] | None:
        entries = self.list_namespace_entries(namespace)
        for item in entries:
            if item.get("archive_sha256") == archive_sha256:
                return item
        return None

    def upsert_snapshot(self, namespace: str, entry: dict[str, Any]) -> None:
        entries = self._load_committed_index(namespace)
        updated = [item for item in entries if item.get("stored_as") != entry["stored_as"]]
        updated.append(self._normalize_entry(namespace, entry))
        self._save_committed_index(namespace, updated)

    def reconcile_namespace(self, namespace: str, existing_snapshots: list[dict[str, Any]]) -> None:
        existing_by_name = {item["filename"]: item for item in existing_snapshots}
        filtered: list[dict[str, Any]] = []
        for item in self._load_committed_index(namespace):
            filename = item.get("stored_as")
            if filename not in existing_by_name:
                continue
            filtered.append(self._normalize_entry(namespace, item, storage_item=existing_by_name[filename]))
        self._save_committed_index(namespace, filtered)

    def list_namespace_entries(self, namespace: str) -> list[dict[str, Any]]:
        entries = self._load_committed_index(namespace)
        normalized = [self._normalize_entry(namespace, item) for item in entries]
        normalized.sort(key=lambda item: (item.get("mtime", 0), item.get("stored_as", "")), reverse=True)
        return normalized

    def list_namespaces(self) -> list[dict[str, Any]]:
        namespaces: list[dict[str, Any]] = []
        if not self.index_root.exists():
            return namespaces

        edge_dirs = sorted(
            [item for item in self.index_root.iterdir() if item.is_dir()],
            key=lambda item: item.name.lower(),
        )
        for edge_dir in edge_dirs:
            jobs: list[dict[str, Any]] = []
            job_dirs = sorted(
                [item for item in edge_dir.iterdir() if item.is_dir()],
                key=lambda item: item.name.lower(),
            )
            for job_dir in job_dirs:
                namespace = f"{edge_dir.name}/{job_dir.name}"
                snapshots = [
                    {
                        "name": item["stored_as"],
                        "size_bytes": item.get("size_bytes", 0),
                        "mtime": item.get("mtime", 0),
                    }
                    for item in self.list_namespace_entries(namespace)
                ]
                if snapshots:
                    jobs.append({"job_name": job_dir.name, "snapshot_count": len(snapshots), "snapshots": snapshots})
            if jobs:
                namespaces.append({"edge_id": edge_dir.name, "jobs": jobs})
        return namespaces

    def get_edge_registration(self, edge_id: str) -> dict[str, Any] | None:
        path = self._registration_path(edge_id)
        if not path.exists():
            return None
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return None
        return payload if isinstance(payload, dict) else None

    def upsert_edge_registration(self, registration: dict[str, Any]) -> None:
        edge_id = str(registration["edge_id"])
        path = self._registration_path(edge_id)
        path.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(
            "w",
            encoding="utf-8",
            dir=path.parent,
            delete=False,
            suffix=".tmp",
        ) as handle:
            json.dump(registration, handle, indent=2, sort_keys=True)
            handle.flush()
            temp_path = Path(handle.name)
        temp_path.replace(path)

    def list_edge_registrations(self) -> list[dict[str, Any]]:
        if not self.registry_root.exists():
            return []
        registrations: list[dict[str, Any]] = []
        for path in sorted(self.registry_root.glob("*.json"), key=lambda item: item.name.lower()):
            try:
                payload = json.loads(path.read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError):
                continue
            if isinstance(payload, dict):
                registrations.append(payload)
        return registrations

    def _index_dir(self, namespace: str) -> Path:
        return self.index_root / namespace

    def _index_path(self, namespace: str) -> Path:
        return self._index_dir(namespace) / "committed.json"

    def _registration_path(self, edge_id: str) -> Path:
        return self.registry_root / f"{edge_id}.json"

    def _load_committed_index(self, namespace: str) -> list[dict[str, Any]]:
        index_path = self._index_path(namespace)
        if not index_path.exists():
            return []
        try:
            payload = json.loads(index_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return []
        if not isinstance(payload, list):
            return []

        entries: list[dict[str, Any]] = []
        for item in payload:
            if not isinstance(item, dict):
                continue
            stored_as = item.get("stored_as")
            archive_sha256 = item.get("archive_sha256")
            if not isinstance(stored_as, str) or not isinstance(archive_sha256, str):
                continue
            entries.append(item)
        return entries

    def _save_committed_index(self, namespace: str, entries: list[dict[str, Any]]) -> None:
        index_dir = self._index_dir(namespace)
        index_path = self._index_path(namespace)
        index_dir.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(
            "w",
            encoding="utf-8",
            dir=index_dir,
            delete=False,
            suffix=".tmp",
        ) as handle:
            json.dump(entries, handle, indent=2, sort_keys=True)
            handle.flush()
            temp_path = Path(handle.name)
        temp_path.replace(index_path)

    def _normalize_entry(
        self,
        namespace: str,
        entry: dict[str, Any],
        *,
        storage_item: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        normalized = dict(entry)
        stored_as = str(normalized["stored_as"])
        storage_data = storage_item or self._storage_item(namespace, stored_as)
        normalized["size_bytes"] = int((storage_data or {}).get("size_bytes") or normalized.get("size_bytes") or 0)
        normalized["mtime"] = float((storage_data or {}).get("mtime") or normalized.get("mtime") or 0)
        return normalized

    def _storage_item(self, namespace: str, stored_as: str) -> dict[str, Any] | None:
        target = self.backup_root / namespace / stored_as
        if not target.is_file():
            return None
        stat_result = target.stat()
        return {"filename": stored_as, "size_bytes": stat_result.st_size, "mtime": stat_result.st_mtime}
