from __future__ import annotations

import hashlib
import sqlite3
import threading
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from app.core.signing import credential_payload, mint_credential, verify_credential


def _utc_now() -> datetime:
    return datetime.now(timezone.utc)


def _token_hash(token: str) -> str:
    return hashlib.sha256(token.encode("utf-8")).hexdigest()


def _expiry_from_payload(payload: dict[str, Any]) -> datetime:
    try:
        return datetime.fromtimestamp(int(payload["exp"]), tz=timezone.utc)
    except Exception as exc:
        raise ValueError("credential missing expiry") from exc


class CredentialStore:
    def __init__(self, database_url: str | None, sqlite_path: Path | None = None) -> None:
        self.database_url = database_url
        self.sqlite_path = sqlite_path
        self._lock = threading.RLock()
        if not self.database_url and self.sqlite_path is None:
            raise RuntimeError("Central credentials require PostgreSQL unless an explicit test SQLite path is provided.")
        self._ensure_schema()

    def mint(self, private_key, ttl_days: int) -> str:
        token = mint_credential(private_key, ttl_days=ttl_days)
        payload = credential_payload(token)
        jti = str(payload["jti"])
        expires_at = _expiry_from_payload(payload)
        token_hash = _token_hash(token)
        with self._lock:
            if self.database_url:
                self._save_postgres(token_hash, jti, expires_at)
            else:
                self._save_sqlite(token_hash, jti, expires_at)
        return token

    def verify(self, token: str, public_key) -> dict[str, Any]:
        payload = verify_credential(token, public_key)
        jti = str(payload["jti"])
        token_hash = _token_hash(token)
        with self._lock:
            record = (
                self._find_active_postgres(token_hash, jti)
                if self.database_url
                else self._find_active_sqlite(token_hash, jti)
            )
        if record is None:
            raise ValueError("credential revoked")
        return {"jti": jti, "token_hash": token_hash, "expires_at": record["expires_at"]}

    def revoke(self, token_hash: str) -> int:
        if not token_hash:
            return 0
        with self._lock:
            return self._delete_postgres(token_hash) if self.database_url else self._delete_sqlite(token_hash)

    def cleanup_expired(self) -> int:
        with self._lock:
            return self._cleanup_expired_postgres() if self.database_url else self._cleanup_expired_sqlite()

    def _ensure_schema(self) -> None:
        if self.database_url:
            with self._connect_postgres() as conn, conn.cursor() as cur:
                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS edge_credentials (
                        token_hash TEXT PRIMARY KEY,
                        jti TEXT NOT NULL UNIQUE,
                        expires_at TIMESTAMPTZ NOT NULL,
                        created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
                    )
                    """
                )
                cur.execute(
                    """
                    CREATE INDEX IF NOT EXISTS idx_edge_credentials_expires_at
                    ON edge_credentials (expires_at)
                    """
                )
            return

        if self.sqlite_path is None:
            raise RuntimeError("SQLite credential path is not configured.")
        self.sqlite_path.parent.mkdir(parents=True, exist_ok=True)
        with self._connect_sqlite() as conn:
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS edge_credentials (
                    token_hash TEXT PRIMARY KEY,
                    jti TEXT NOT NULL UNIQUE,
                    expires_at TEXT NOT NULL,
                    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                )
                """
            )
            conn.execute(
                """
                CREATE INDEX IF NOT EXISTS idx_edge_credentials_expires_at
                ON edge_credentials (expires_at)
                """
            )

    def _save_postgres(self, token_hash: str, jti: str, expires_at: datetime) -> None:
        with self._connect_postgres() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO edge_credentials (token_hash, jti, expires_at)
                VALUES (%s, %s, %s)
                ON CONFLICT (token_hash)
                DO UPDATE SET jti = EXCLUDED.jti, expires_at = EXCLUDED.expires_at
                """,
                (token_hash, jti, expires_at),
            )

    def _save_sqlite(self, token_hash: str, jti: str, expires_at: datetime) -> None:
        with self._connect_sqlite() as conn:
            conn.execute(
                """
                INSERT INTO edge_credentials (token_hash, jti, expires_at)
                VALUES (?, ?, ?)
                ON CONFLICT(token_hash)
                DO UPDATE SET jti = excluded.jti, expires_at = excluded.expires_at
                """,
                (token_hash, jti, expires_at.isoformat()),
            )

    def _find_active_postgres(self, token_hash: str, jti: str) -> dict[str, Any] | None:
        with self._connect_postgres() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT token_hash, jti, expires_at
                FROM edge_credentials
                WHERE token_hash = %s AND jti = %s AND expires_at >= CURRENT_TIMESTAMP
                """,
                (token_hash, jti),
            )
            row = cur.fetchone()
        return _record_from_row(row)

    def _find_active_sqlite(self, token_hash: str, jti: str) -> dict[str, Any] | None:
        with self._connect_sqlite() as conn:
            row = conn.execute(
                """
                SELECT token_hash, jti, expires_at
                FROM edge_credentials
                WHERE token_hash = ? AND jti = ? AND expires_at >= ?
                """,
                (token_hash, jti, _utc_now().isoformat()),
            ).fetchone()
        return _record_from_row(row)

    def _delete_postgres(self, token_hash: str) -> int:
        with self._connect_postgres() as conn, conn.cursor() as cur:
            cur.execute("DELETE FROM edge_credentials WHERE token_hash = %s", (token_hash,))
            return int(cur.rowcount or 0)

    def _delete_sqlite(self, token_hash: str) -> int:
        with self._connect_sqlite() as conn:
            cur = conn.execute("DELETE FROM edge_credentials WHERE token_hash = ?", (token_hash,))
            return int(cur.rowcount or 0)

    def _cleanup_expired_postgres(self) -> int:
        with self._connect_postgres() as conn, conn.cursor() as cur:
            cur.execute("DELETE FROM edge_credentials WHERE expires_at < CURRENT_TIMESTAMP")
            return int(cur.rowcount or 0)

    def _cleanup_expired_sqlite(self) -> int:
        with self._connect_sqlite() as conn:
            cur = conn.execute("DELETE FROM edge_credentials WHERE expires_at < ?", (_utc_now().isoformat(),))
            return int(cur.rowcount or 0)

    def _connect_postgres(self):
        import psycopg

        return psycopg.connect(self.database_url, autocommit=True)

    def _connect_sqlite(self) -> sqlite3.Connection:
        if self.sqlite_path is None:
            raise RuntimeError("SQLite credential path is not configured.")
        return sqlite3.connect(self.sqlite_path)


def _record_from_row(row) -> dict[str, Any] | None:
    if row is None:
        return None
    return {"token_hash": row[0], "jti": row[1], "expires_at": row[2]}
