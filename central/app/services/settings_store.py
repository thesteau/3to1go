from __future__ import annotations

import json
import tempfile
import threading
from pathlib import Path
from typing import Any

from app.core.config import Settings, build_settings, settings_storage_path, settings_to_payload


class SettingsStore:
    def __init__(self, path: Path | None = None) -> None:
        self.path = path or settings_storage_path()
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self._lock = threading.RLock()

    def snapshot(self, settings: Settings) -> dict[str, Any]:
        with self._lock:
            return settings_to_payload(settings)

    def save(self, payload: dict[str, Any]) -> Settings:
        settings = build_settings(payload)
        serialized = settings_to_payload(settings)
        with self._lock:
            with tempfile.NamedTemporaryFile(
                "w",
                encoding="utf-8",
                dir=self.path.parent,
                delete=False,
                suffix=".tmp",
            ) as handle:
                json.dump(serialized, handle, indent=2, sort_keys=True)
                handle.flush()
                temp_path = Path(handle.name)
            temp_path.replace(self.path)
        return settings
