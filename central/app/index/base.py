from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any


class SnapshotIndexBackend(ABC):
    backend_name = "unknown"

    @abstractmethod
    def find_duplicate(self, namespace: str, archive_sha256: str) -> dict[str, Any] | None:
        raise NotImplementedError

    @abstractmethod
    def upsert_snapshot(self, namespace: str, entry: dict[str, Any]) -> None:
        raise NotImplementedError

    @abstractmethod
    def reconcile_namespace(self, namespace: str, existing_snapshots: list[dict[str, Any]]) -> None:
        raise NotImplementedError

    @abstractmethod
    def list_namespace_entries(self, namespace: str) -> list[dict[str, Any]]:
        raise NotImplementedError

    @abstractmethod
    def list_namespaces(self) -> list[dict[str, Any]]:
        raise NotImplementedError

    @abstractmethod
    def get_edge_registration(self, edge_id: str, edge_instance_id: str) -> dict[str, Any] | None:
        raise NotImplementedError

    @abstractmethod
    def upsert_edge_registration(self, registration: dict[str, Any]) -> None:
        raise NotImplementedError

    @abstractmethod
    def list_edge_registrations(self, edge_id: str | None = None) -> list[dict[str, Any]]:
        raise NotImplementedError
