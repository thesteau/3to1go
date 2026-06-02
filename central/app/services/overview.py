from __future__ import annotations

from app.core.config import Settings, settings_storage_path, settings_to_payload
from app.index.base import SnapshotIndexBackend
from app.storage.local import LocalFilesystemBackend


def build_overview(
    settings: Settings,
    storage_backend: LocalFilesystemBackend,
    snapshot_index: SnapshotIndexBackend,
) -> dict:
    edges: list[dict] = []
    edge_map: dict[str, dict] = {}
    instance_map: dict[tuple[str, str | None], dict] = {}

    for registration in snapshot_index.list_edge_registrations():
        edge = edge_map.get(registration["edge_id"])
        if edge is None:
            edge = {"edge_id": registration["edge_id"], "instances": []}
            edge_map[registration["edge_id"]] = edge
            edges.append(edge)

        key = (registration["edge_id"], registration.get("edge_instance_id"))
        instance = instance_map.get(key)
        if instance is None:
            instance = {
                "edge_instance_id": registration.get("edge_instance_id"),
                "instance_label": registration.get("edge_instance_id", ""),
                "advertised_url": registration.get("advertised_url"),
                "encryption_key_fingerprint": registration.get("encryption_key_fingerprint"),
                "first_seen_at": registration.get("first_seen_at"),
                "last_seen_at": registration.get("last_seen_at"),
                "jobs": [],
            }
            instance_map[key] = instance
            edge["instances"].append(instance)
        else:
            if registration.get("advertised_url"):
                instance["advertised_url"] = registration.get("advertised_url")
            if registration.get("encryption_key_fingerprint"):
                instance["encryption_key_fingerprint"] = registration.get("encryption_key_fingerprint")
            instance["first_seen_at"] = (
                registration.get("first_seen_at") or instance.get("first_seen_at")
            )
            instance["last_seen_at"] = (
                registration.get("last_seen_at") or instance.get("last_seen_at")
            )

    for namespace in snapshot_index.list_namespaces():
        edge = edge_map.get(namespace["edge_id"])
        if edge is None:
            edge = {"edge_id": namespace["edge_id"], "instances": []}
            edge_map[namespace["edge_id"]] = edge
            edges.append(edge)

        key = (namespace["edge_id"], namespace.get("edge_instance_id"))
        instance = instance_map.get(key)
        if instance is None:
            instance = {
                "edge_instance_id": namespace.get("edge_instance_id"),
                "instance_label": namespace.get("edge_instance_id", ""),
                "advertised_url": None,
                "encryption_key_fingerprint": None,
                "first_seen_at": None,
                "last_seen_at": None,
                "jobs": [],
            }
            instance_map[key] = instance
            edge["instances"].append(instance)
        instance["jobs"] = namespace["jobs"]

    return {
        "status": "ok" if storage_backend.healthcheck() else "degraded",
        "backup_root": str(settings.backup_root),
        "staging_dir": str(settings.staging_dir),
        "retention_keep_last": settings.retention_keep_last,
        "settings_path": str(settings_storage_path()),
        "settings": settings_to_payload(settings),
        "http_url": f"http://localhost:{settings.http_port}",
        "edges": edges,
    }
