from __future__ import annotations

import hashlib
import hmac
import secrets
import sqlite3
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any

from app.core.config import app_database_path


DEFAULT_ADMIN_USERNAME = "admin"
DEFAULT_ADMIN_PASSWORD = "admin"
SESSION_COOKIE = "relay_session"
SESSION_DAYS = 7


class UserStore:
    def __init__(self, database_url: str | None = None, sqlite_path: Path | None = None) -> None:
        self.database_url = database_url
        self.sqlite_path = sqlite_path or app_database_path()
        self._ensure_schema()
        self._ensure_default_admin()

    def authenticate(self, username: str, password: str) -> dict[str, Any] | None:
        user = self.get_user_by_username(username)
        if user is None or not _verify_password(password, user["password_hash"]):
            return None
        return _public_user(user)

    def create_session(self, user_id: int) -> str:
        token = secrets.token_urlsafe(32)
        expires_at = _utc_now() + timedelta(days=SESSION_DAYS)
        if self.database_url:
            with self._connect_postgres() as conn, conn.cursor() as cur:
                cur.execute(
                    "INSERT INTO app_sessions (token, user_id, expires_at) VALUES (%s, %s, %s)",
                    (token, user_id, expires_at),
                )
        else:
            with self._connect_sqlite() as conn:
                conn.execute(
                    "INSERT INTO app_sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
                    (token, user_id, expires_at.isoformat()),
                )
        return token

    def delete_session(self, token: str) -> None:
        if not token:
            return
        if self.database_url:
            with self._connect_postgres() as conn, conn.cursor() as cur:
                cur.execute("DELETE FROM app_sessions WHERE token = %s", (token,))
        else:
            with self._connect_sqlite() as conn:
                conn.execute("DELETE FROM app_sessions WHERE token = ?", (token,))

    def user_for_session(self, token: str | None) -> dict[str, Any] | None:
        if not token:
            return None
        self._delete_expired_sessions()
        if self.database_url:
            with self._connect_postgres() as conn, conn.cursor() as cur:
                cur.execute(
                    """
                    SELECT u.id, u.username, u.password_hash, u.is_admin, u.must_change_password, u.created_at
                    FROM app_sessions s
                    JOIN app_users u ON u.id = s.user_id
                    WHERE s.token = %s AND s.expires_at >= CURRENT_TIMESTAMP
                    """,
                    (token,),
                )
                row = cur.fetchone()
        else:
            with self._connect_sqlite() as conn:
                row = conn.execute(
                    """
                    SELECT u.id, u.username, u.password_hash, u.is_admin, u.must_change_password, u.created_at
                    FROM app_sessions s
                    JOIN app_users u ON u.id = s.user_id
                    WHERE s.token = ? AND s.expires_at >= ?
                    """,
                    (token, _utc_now().isoformat()),
                ).fetchone()
        return _public_user(_row_to_user(row)) if row else None

    def list_users(self) -> list[dict[str, Any]]:
        rows = self._fetch_all_users()
        return [_public_user(_row_to_user(row)) for row in rows]

    def get_user_by_id(self, user_id: int) -> dict[str, Any] | None:
        row = self._fetch_user("id", user_id)
        return _row_to_user(row) if row else None

    def get_user_by_username(self, username: str) -> dict[str, Any] | None:
        row = self._fetch_user("username", username.strip().lower())
        return _row_to_user(row) if row else None

    def create_user(self, username: str, password: str, is_admin: bool = False) -> dict[str, Any]:
        normalized = _normalize_username(username)
        password_hash = _hash_password(password)
        try:
            if self.database_url:
                with self._connect_postgres() as conn, conn.cursor() as cur:
                    cur.execute(
                        """
                        INSERT INTO app_users (username, password_hash, is_admin, must_change_password)
                        VALUES (%s, %s, %s, %s)
                        RETURNING id, username, password_hash, is_admin, must_change_password, created_at
                        """,
                        (normalized, password_hash, is_admin, False),
                    )
                    row = cur.fetchone()
            else:
                with self._connect_sqlite() as conn:
                    cur = conn.execute(
                        """
                        INSERT INTO app_users (username, password_hash, is_admin, must_change_password)
                        VALUES (?, ?, ?, ?)
                        """,
                        (normalized, password_hash, int(is_admin), 0),
                    )
                    row = conn.execute(
                        """
                        SELECT id, username, password_hash, is_admin, must_change_password, created_at
                        FROM app_users WHERE id = ?
                        """,
                        (cur.lastrowid,),
                    ).fetchone()
        except Exception as exc:
            raise ValueError("username already exists") from exc
        return _public_user(_row_to_user(row))

    def update_user(
        self,
        user_id: int,
        *,
        username: str | None = None,
        password: str | None = None,
        is_admin: bool | None = None,
        must_change_password: bool | None = None,
    ) -> dict[str, Any]:
        existing = self.get_user_by_id(user_id)
        if existing is None:
            raise ValueError("user not found")
        next_username = _normalize_username(username) if username is not None else existing["username"]
        next_hash = _hash_password(password) if password else existing["password_hash"]
        next_admin = existing["is_admin"] if is_admin is None else bool(is_admin)
        next_must_change = existing["must_change_password"] if must_change_password is None else bool(must_change_password)
        if existing["username"] == DEFAULT_ADMIN_USERNAME:
            next_admin = True
        if self._would_remove_last_admin(user_id, next_admin):
            raise ValueError("at least one admin is required")
        try:
            if self.database_url:
                with self._connect_postgres() as conn, conn.cursor() as cur:
                    cur.execute(
                        """
                        UPDATE app_users
                        SET username = %s, password_hash = %s, is_admin = %s, must_change_password = %s
                        WHERE id = %s
                        RETURNING id, username, password_hash, is_admin, must_change_password, created_at
                        """,
                        (next_username, next_hash, next_admin, next_must_change, user_id),
                    )
                    row = cur.fetchone()
            else:
                with self._connect_sqlite() as conn:
                    conn.execute(
                        """
                        UPDATE app_users
                        SET username = ?, password_hash = ?, is_admin = ?, must_change_password = ?
                        WHERE id = ?
                        """,
                        (next_username, next_hash, int(next_admin), int(next_must_change), user_id),
                    )
                    row = conn.execute(
                        """
                        SELECT id, username, password_hash, is_admin, must_change_password, created_at
                        FROM app_users WHERE id = ?
                        """,
                        (user_id,),
                    ).fetchone()
        except Exception as exc:
            raise ValueError("username already exists") from exc
        return _public_user(_row_to_user(row))

    def delete_user(self, user_id: int) -> None:
        existing = self.get_user_by_id(user_id)
        if existing is None:
            raise ValueError("user not found")
        if existing["username"] == DEFAULT_ADMIN_USERNAME:
            raise ValueError("the bootstrap admin user cannot be removed")
        if self._would_remove_last_admin(user_id, False):
            raise ValueError("at least one admin is required")
        if self.database_url:
            with self._connect_postgres() as conn, conn.cursor() as cur:
                cur.execute("DELETE FROM app_sessions WHERE user_id = %s", (user_id,))
                cur.execute("DELETE FROM app_users WHERE id = %s", (user_id,))
        else:
            with self._connect_sqlite() as conn:
                conn.execute("DELETE FROM app_sessions WHERE user_id = ?", (user_id,))
                conn.execute("DELETE FROM app_users WHERE id = ?", (user_id,))

    def change_password(self, user_id: int, current_password: str | None, new_password: str, *, require_current: bool) -> dict[str, Any]:
        user = self.get_user_by_id(user_id)
        if user is None:
            raise ValueError("user not found")
        if require_current and not _verify_password(current_password or "", user["password_hash"]):
            raise ValueError("current password is incorrect")
        return self.update_user(user_id, password=new_password, must_change_password=False)

    def _ensure_schema(self) -> None:
        if self.database_url:
            with self._connect_postgres() as conn, conn.cursor() as cur:
                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS app_users (
                        id SERIAL PRIMARY KEY,
                        username TEXT NOT NULL UNIQUE,
                        password_hash TEXT NOT NULL,
                        is_admin BOOLEAN NOT NULL DEFAULT FALSE,
                        must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
                        created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
                    )
                    """
                )
                cur.execute(
                    """
                    CREATE TABLE IF NOT EXISTS app_sessions (
                        token TEXT PRIMARY KEY,
                        user_id INTEGER NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
                        expires_at TIMESTAMPTZ NOT NULL,
                        created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
                    )
                    """
                )
        else:
            self.sqlite_path.parent.mkdir(parents=True, exist_ok=True)
            with self._connect_sqlite() as conn:
                conn.execute(
                    """
                    CREATE TABLE IF NOT EXISTS app_users (
                        id INTEGER PRIMARY KEY AUTOINCREMENT,
                        username TEXT NOT NULL UNIQUE,
                        password_hash TEXT NOT NULL,
                        is_admin INTEGER NOT NULL DEFAULT 0,
                        must_change_password INTEGER NOT NULL DEFAULT 0,
                        created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                    )
                    """
                )
                conn.execute(
                    """
                    CREATE TABLE IF NOT EXISTS app_sessions (
                        token TEXT PRIMARY KEY,
                        user_id INTEGER NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
                        expires_at TEXT NOT NULL,
                        created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                    )
                    """
                )

    def _ensure_default_admin(self) -> None:
        if self._fetch_all_users():
            return
        self.create_user(DEFAULT_ADMIN_USERNAME, DEFAULT_ADMIN_PASSWORD, is_admin=True)
        admin = self.get_user_by_username(DEFAULT_ADMIN_USERNAME)
        if admin:
            self.update_user(admin["id"], must_change_password=True)

    def _fetch_all_users(self):
        if self.database_url:
            with self._connect_postgres() as conn, conn.cursor() as cur:
                cur.execute(
                    """
                    SELECT id, username, password_hash, is_admin, must_change_password, created_at
                    FROM app_users ORDER BY lower(username)
                    """
                )
                return cur.fetchall()
        with self._connect_sqlite() as conn:
            return conn.execute(
                """
                SELECT id, username, password_hash, is_admin, must_change_password, created_at
                FROM app_users ORDER BY lower(username)
                """
            ).fetchall()

    def _fetch_user(self, field: str, value: Any):
        if field not in {"id", "username"}:
            raise ValueError("invalid user lookup")
        if self.database_url:
            with self._connect_postgres() as conn, conn.cursor() as cur:
                cur.execute(
                    f"SELECT id, username, password_hash, is_admin, must_change_password, created_at FROM app_users WHERE {field} = %s",
                    (value,),
                )
                return cur.fetchone()
        with self._connect_sqlite() as conn:
            return conn.execute(
                f"SELECT id, username, password_hash, is_admin, must_change_password, created_at FROM app_users WHERE {field} = ?",
                (value,),
            ).fetchone()

    def _would_remove_last_admin(self, user_id: int, next_admin: bool) -> bool:
        admins = [user for user in self.list_users() if user["is_admin"]]
        return not next_admin and len(admins) == 1 and admins[0]["id"] == user_id

    def _delete_expired_sessions(self) -> None:
        if self.database_url:
            with self._connect_postgres() as conn, conn.cursor() as cur:
                cur.execute("DELETE FROM app_sessions WHERE expires_at < CURRENT_TIMESTAMP")
        else:
            with self._connect_sqlite() as conn:
                conn.execute("DELETE FROM app_sessions WHERE expires_at < ?", (_utc_now().isoformat(),))

    def _connect_postgres(self):
        import psycopg

        return psycopg.connect(self.database_url, autocommit=True)

    def _connect_sqlite(self) -> sqlite3.Connection:
        conn = sqlite3.connect(self.sqlite_path)
        conn.execute("PRAGMA foreign_keys = ON")
        return conn


def _normalize_username(username: str) -> str:
    normalized = username.strip().lower()
    if len(normalized) < 3:
        raise ValueError("username must be at least 3 characters")
    if len(normalized) > 64:
        raise ValueError("username must be at most 64 characters")
    if not all(ch.isalnum() or ch in {"_", "-", "."} for ch in normalized):
        raise ValueError("username can only contain letters, numbers, dots, dashes, and underscores")
    return normalized


def _hash_password(password: str) -> str:
    if len(password) < 4:
        raise ValueError("password must be at least 4 characters")
    salt = secrets.token_bytes(16)
    digest = hashlib.pbkdf2_hmac("sha256", password.encode("utf-8"), salt, 260_000)
    return f"pbkdf2_sha256$260000${salt.hex()}${digest.hex()}"


def _verify_password(password: str, encoded: str) -> bool:
    try:
        algorithm, iterations_text, salt_hex, digest_hex = encoded.split("$", 3)
        if algorithm != "pbkdf2_sha256":
            return False
        salt = bytes.fromhex(salt_hex)
        expected = bytes.fromhex(digest_hex)
        digest = hashlib.pbkdf2_hmac("sha256", password.encode("utf-8"), salt, int(iterations_text))
        return hmac.compare_digest(digest, expected)
    except (ValueError, TypeError):
        return False


def _row_to_user(row) -> dict[str, Any]:
    return {
        "id": int(row[0]),
        "username": row[1],
        "password_hash": row[2],
        "is_admin": bool(row[3]),
        "must_change_password": bool(row[4]),
        "created_at": str(row[5]),
    }


def _public_user(user: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": user["id"],
        "username": user["username"],
        "is_admin": user["is_admin"],
        "must_change_password": user["must_change_password"],
        "created_at": user["created_at"],
    }


def _utc_now() -> datetime:
    return datetime.now(timezone.utc)
