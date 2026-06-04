from __future__ import annotations

import json
import sqlite3
import threading
from typing import Any

from app.core.config import Settings, app_database_path, build_settings, settings_to_payload


class SettingsStore:
    def __init__(self) -> None:
        self.sqlite_path = app_database_path()
        self._lock = threading.RLock()
        self._ensure_schema()

    def snapshot(self, settings: Settings) -> dict[str, Any]:
        with self._lock:
            return settings_to_payload(settings)

    def save(self, payload: dict[str, Any]) -> Settings:
        settings = build_settings(payload)
        serialized = settings_to_payload(settings)
        with self._lock:
            self._save_sqlite(serialized)
        return settings

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
