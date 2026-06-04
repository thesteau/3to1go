from __future__ import annotations

import json
import sqlite3
import threading
from pathlib import Path
from typing import Any

from app.core.config import Settings, build_settings, settings_to_payload


class SettingsStore:
    def __init__(self, database_url: str | None, sqlite_path: Path | None = None) -> None:
        self.database_url = database_url
        self.sqlite_path = sqlite_path
        self._lock = threading.RLock()
        if not self.database_url and self.sqlite_path is None:
            raise RuntimeError("Central settings require PostgreSQL unless an explicit test SQLite path is provided.")
        self._ensure_schema()

    def snapshot(self, settings: Settings) -> dict[str, Any]:
        with self._lock:
            return settings_to_payload(settings)

    def save(self, payload: dict[str, Any]) -> Settings:
        settings = build_settings(payload)
        serialized = settings_to_payload(settings)
        with self._lock:
            if self.database_url:
                self._save_postgres(serialized)
            else:
                self._save_sqlite(serialized)
        return settings

    def _ensure_schema(self) -> None:
        if self.database_url:
            self._ensure_postgres_schema()
        else:
            self._ensure_sqlite_schema()

    def _ensure_postgres_schema(self) -> None:
        with self._connect_postgres() as conn, conn.cursor() as cur:
            cur.execute(
                """
                CREATE TABLE IF NOT EXISTS app_settings (
                    key TEXT PRIMARY KEY,
                    payload JSONB NOT NULL,
                    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
                )
                """
            )

    def _save_postgres(self, serialized: dict[str, Any]) -> None:
        from psycopg.types.json import Json

        with self._connect_postgres() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO app_settings (key, payload, updated_at)
                VALUES (%s, %s, CURRENT_TIMESTAMP)
                ON CONFLICT (key)
                DO UPDATE SET payload = EXCLUDED.payload, updated_at = CURRENT_TIMESTAMP
                """,
                ("settings", Json(serialized)),
            )

    def _connect_postgres(self):
        import psycopg

        return psycopg.connect(self.database_url, autocommit=True)

    def _ensure_sqlite_schema(self) -> None:
        if self.sqlite_path is None:
            raise RuntimeError("SQLite settings path is not configured.")
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
        if self.sqlite_path is None:
            raise RuntimeError("SQLite settings path is not configured.")
        return sqlite3.connect(self.sqlite_path)
