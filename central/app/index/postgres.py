from __future__ import annotations

from typing import Any

from app.index.base import SnapshotIndexBackend


class PostgresSnapshotIndexBackend(SnapshotIndexBackend):
    backend_name = "postgres"

    def __init__(self, database_url: str) -> None:
        self.database_url = database_url
        self._ensure_schema()

    def find_duplicate(self, namespace: str, archive_sha256: str) -> dict[str, Any] | None:
        edge_id, edge_instance_id, job_name = _split_namespace(namespace)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT stored_as, archive_sha256, fingerprint, snapshot_timestamp, size_bytes, mtime
                FROM snapshot_index
                WHERE edge_id = %s AND edge_instance_id = %s AND job_name = %s AND archive_sha256 = %s
                ORDER BY updated_at DESC
                LIMIT 1
                """,
                (edge_id, edge_instance_id, job_name, archive_sha256),
            )
            row = cur.fetchone()
        if row is None:
            return None
        return {
            "stored_as": row[0],
            "archive_sha256": row[1],
            "fingerprint": row[2],
            "timestamp": row[3],
            "size_bytes": int(row[4] or 0),
            "mtime": float(row[5] or 0),
        }

    def upsert_snapshot(self, namespace: str, entry: dict[str, Any]) -> None:
        edge_id, edge_instance_id, job_name = _split_namespace(namespace)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO snapshot_index (
                    edge_id,
                    edge_instance_id,
                    job_name,
                    stored_as,
                    archive_sha256,
                    fingerprint,
                    snapshot_timestamp,
                    size_bytes,
                    mtime
                )
                VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
                ON CONFLICT (edge_id, edge_instance_id, job_name, stored_as)
                DO UPDATE SET
                    archive_sha256 = EXCLUDED.archive_sha256,
                    fingerprint = EXCLUDED.fingerprint,
                    snapshot_timestamp = EXCLUDED.snapshot_timestamp,
                    size_bytes = EXCLUDED.size_bytes,
                    mtime = EXCLUDED.mtime,
                    updated_at = CURRENT_TIMESTAMP
                """,
                (
                    edge_id,
                    edge_instance_id,
                    job_name,
                    str(entry["stored_as"]),
                    str(entry["archive_sha256"]),
                    entry.get("fingerprint"),
                    entry.get("timestamp"),
                    int(entry.get("size_bytes") or 0),
                    float(entry.get("mtime") or 0),
                ),
            )

    def reconcile_namespace(self, namespace: str, existing_snapshots: list[dict[str, Any]]) -> None:
        edge_id, edge_instance_id, job_name = _split_namespace(namespace)
        filenames = [str(item["filename"]) for item in existing_snapshots]
        with self._connect() as conn, conn.cursor() as cur:
            if filenames:
                cur.execute(
                    """
                    DELETE FROM snapshot_index
                    WHERE edge_id = %s AND edge_instance_id = %s AND job_name = %s AND NOT (stored_as = ANY(%s))
                    """,
                    (edge_id, edge_instance_id, job_name, filenames),
                )
            else:
                cur.execute(
                    "DELETE FROM snapshot_index WHERE edge_id = %s AND edge_instance_id = %s AND job_name = %s",
                    (edge_id, edge_instance_id, job_name),
                )

    def list_namespace_entries(self, namespace: str) -> list[dict[str, Any]]:
        edge_id, edge_instance_id, job_name = _split_namespace(namespace)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT stored_as, archive_sha256, fingerprint, snapshot_timestamp, size_bytes, mtime
                FROM snapshot_index
                WHERE edge_id = %s AND edge_instance_id = %s AND job_name = %s
                ORDER BY mtime DESC, stored_as DESC
                """,
                (edge_id, edge_instance_id, job_name),
            )
            rows = cur.fetchall()
        return [
            {
                "stored_as": row[0],
                "archive_sha256": row[1],
                "fingerprint": row[2],
                "timestamp": row[3],
                "size_bytes": int(row[4] or 0),
                "mtime": float(row[5] or 0),
            }
            for row in rows
        ]

    def list_namespaces(self) -> list[dict[str, Any]]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT edge_id, edge_instance_id, job_name, stored_as, size_bytes, mtime
                FROM snapshot_index
                ORDER BY lower(edge_id), lower(edge_instance_id), lower(job_name), mtime DESC, stored_as DESC
                """
            )
            rows = cur.fetchall()

        instances: list[dict[str, Any]] = []
        instance_map: dict[tuple[str, str | None], dict[str, Any]] = {}
        for edge_id, raw_instance_id, job_name, stored_as, size_bytes, mtime in rows:
            key = (edge_id, raw_instance_id)
            instance = instance_map.get(key)
            if instance is None:
                instance = {"edge_id": edge_id, "edge_instance_id": raw_instance_id, "jobs": [], "_job_map": {}}
                instance_map[key] = instance
                instances.append(instance)
            job = instance["_job_map"].get(job_name)
            if job is None:
                job = {"job_name": job_name, "snapshot_count": 0, "snapshots": []}
                instance["_job_map"][job_name] = job
                instance["jobs"].append(job)
            job["snapshot_count"] += 1
            job["snapshots"].append(
                {"name": stored_as, "size_bytes": int(size_bytes or 0), "mtime": float(mtime or 0)}
            )

        for instance in instances:
            instance.pop("_job_map", None)
        return instances

    def get_edge_registration(self, edge_id: str, edge_instance_id: str) -> dict[str, Any] | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT edge_id, edge_instance_id, encryption_key_fingerprint,
                       advertised_url, first_seen_at, last_seen_at
                FROM edge_registration
                WHERE edge_id = %s AND edge_instance_id = %s
                """,
                (edge_id, edge_instance_id),
            )
            row = cur.fetchone()
        if row is None:
            return None
        return {
            "edge_id": row[0],
            "edge_instance_id": row[1],
            "encryption_key_fingerprint": row[2],
            "advertised_url": row[3],
            "first_seen_at": row[4],
            "last_seen_at": row[5],
        }

    def upsert_edge_registration(self, registration: dict[str, Any]) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO edge_registration (
                    edge_id,
                    edge_instance_id,
                    encryption_key_fingerprint,
                    advertised_url,
                    first_seen_at,
                    last_seen_at
                )
                VALUES (%s, %s, %s, %s, %s, %s)
                ON CONFLICT (edge_id, edge_instance_id)
                DO UPDATE SET
                    encryption_key_fingerprint = EXCLUDED.encryption_key_fingerprint,
                    advertised_url = EXCLUDED.advertised_url,
                    first_seen_at = EXCLUDED.first_seen_at,
                    last_seen_at = EXCLUDED.last_seen_at
                """,
                (
                    registration["edge_id"],
                    registration["edge_instance_id"],
                    registration.get("encryption_key_fingerprint"),
                    registration.get("advertised_url"),
                    registration["first_seen_at"],
                    registration["last_seen_at"],
                ),
            )

    def delete_edge_registration(self, edge_id: str, edge_instance_id: str) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "DELETE FROM edge_registration WHERE edge_id = %s AND edge_instance_id = %s",
                (edge_id, edge_instance_id),
            )

    def list_edge_registrations(self, edge_id: str | None = None) -> list[dict[str, Any]]:
        with self._connect() as conn, conn.cursor() as cur:
            if edge_id is None:
                cur.execute(
                    """
                    SELECT edge_id, edge_instance_id, encryption_key_fingerprint,
                           advertised_url, first_seen_at, last_seen_at
                    FROM edge_registration
                    ORDER BY lower(edge_id), lower(edge_instance_id)
                    """
                )
            else:
                cur.execute(
                    """
                    SELECT edge_id, edge_instance_id, encryption_key_fingerprint,
                           advertised_url, first_seen_at, last_seen_at
                    FROM edge_registration
                    WHERE edge_id = %s
                    ORDER BY lower(edge_instance_id)
                    """,
                    (edge_id,),
                )
            rows = cur.fetchall()
        return [
            {
                "edge_id": row[0],
                "edge_instance_id": row[1],
                "encryption_key_fingerprint": row[2],
                "advertised_url": row[3],
                "first_seen_at": row[4],
                "last_seen_at": row[5],
            }
            for row in rows
        ]

    def _ensure_schema(self) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                CREATE TABLE IF NOT EXISTS snapshot_index (
                    edge_id TEXT NOT NULL,
                    edge_instance_id TEXT NOT NULL,
                    job_name TEXT NOT NULL,
                    stored_as TEXT NOT NULL,
                    archive_sha256 TEXT NOT NULL,
                    fingerprint TEXT,
                    snapshot_timestamp TEXT,
                    size_bytes BIGINT NOT NULL DEFAULT 0,
                    mtime DOUBLE PRECISION NOT NULL DEFAULT 0,
                    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
                )
                """
            )
            cur.execute(
                """
                CREATE TABLE IF NOT EXISTS edge_registration (
                    edge_id TEXT NOT NULL,
                    edge_instance_id TEXT NOT NULL,
                    encryption_key_fingerprint TEXT,
                    advertised_url TEXT,
                    first_seen_at TEXT NOT NULL,
                    last_seen_at TEXT NOT NULL
                )
                """
            )
            cur.execute(
                """
                CREATE UNIQUE INDEX IF NOT EXISTS idx_snapshot_index_namespace_pk
                ON snapshot_index (edge_id, edge_instance_id, job_name, stored_as)
                """
            )
            cur.execute(
                """
                CREATE INDEX IF NOT EXISTS idx_snapshot_index_namespace_sha
                ON snapshot_index (edge_id, edge_instance_id, job_name, archive_sha256)
                """
            )
            cur.execute(
                """
                CREATE INDEX IF NOT EXISTS idx_snapshot_index_namespace_mtime
                ON snapshot_index (edge_id, edge_instance_id, job_name, mtime DESC, stored_as DESC)
                """
            )
            cur.execute(
                """
                CREATE UNIQUE INDEX IF NOT EXISTS idx_edge_registration_instance
                ON edge_registration (edge_id, edge_instance_id)
                """
            )

    def _connect(self):
        try:
            import psycopg
        except ImportError as exc:
            raise RuntimeError("psycopg is required for the postgres snapshot index backend") from exc
        return psycopg.connect(self.database_url, autocommit=True)


def _split_namespace(namespace: str) -> tuple[str, str, str]:
    parts = namespace.split("/")
    if len(parts) == 3 and all(parts):
        return parts[0], parts[1], parts[2]
    raise ValueError(f"invalid namespace: {namespace}")
