from __future__ import annotations

from app.storage.base import StorageBackend


def prune_old_snapshots(
    storage_backend: StorageBackend,
    namespace: str,
    keep_last: int,
) -> int:
    snapshots = storage_backend.list(namespace)
    if len(snapshots) <= keep_last:
        return 0

    snapshots.sort(key=lambda item: (item["mtime"], item["filename"]), reverse=True)
    to_delete = snapshots[keep_last:]

    for snapshot in to_delete:
        storage_backend.delete(namespace, snapshot["filename"])

    return len(to_delete)

