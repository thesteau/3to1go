from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path


class StorageBackend(ABC):
    @abstractmethod
    def store(self, namespace: str, filename: str, staged_path: Path) -> dict:
        raise NotImplementedError

    @abstractmethod
    def list(self, namespace: str) -> list[dict]:
        raise NotImplementedError

    @abstractmethod
    def delete(self, namespace: str, filename: str) -> None:
        raise NotImplementedError

    @abstractmethod
    def healthcheck(self) -> bool:
        raise NotImplementedError

