from __future__ import annotations

from pathlib import Path

import requests
from requests.exceptions import RequestException


class UploadError(RuntimeError):
    pass


class UploadClient:
    def __init__(self, central_url: str, auth_token: str) -> None:
        self.central_url = central_url.rstrip("/")
        self.auth_token = auth_token

    def _fetch_central_health(self) -> dict[str, int] | None:
        try:
            response = requests.get(f"{self.central_url}/health", timeout=(5, 15))
            if response.ok:
                return response.json()
        except RequestException:
            pass
        return None

    def _validate_capacity(self, archive_path: Path, health: dict[str, int]) -> None:
        archive_size = archive_path.stat().st_size
        max_upload = health.get("max_upload_size_bytes")
        if max_upload is not None and archive_size > max_upload:
            raise UploadError(
                f"archive size {archive_size} exceeds central max upload size {max_upload}"
            )

        staging_free = health.get("staging_free_bytes")
        backup_free = health.get("backup_free_bytes")
        if staging_free is not None and archive_size > staging_free:
            raise UploadError(
                f"archive size {archive_size} exceeds central staging free space {staging_free}"
            )
        if backup_free is not None and archive_size > backup_free:
            raise UploadError(
                f"archive size {archive_size} exceeds central backup free space {backup_free}"
            )

    def upload_archive(
        self,
        edge_id: str,
        job_name: str,
        fingerprint: str,
        timestamp: str,
        archive_path: Path,
    ) -> dict:
        health = self._fetch_central_health()
        if health is not None:
            self._validate_capacity(archive_path, health)

        with archive_path.open("rb") as handle:
            response = requests.post(
                f"{self.central_url}/backup/upload",
                headers={"Authorization": f"Bearer {self.auth_token}"},
                data={
                    "edge_id": edge_id,
                    "job_name": job_name,
                    "fingerprint": fingerprint,
                    "timestamp": timestamp,
                    "archive_format": "tar.zst",
                },
                files={
                    "archive": (archive_path.name, handle, "application/zstd"),
                },
                timeout=(10, 3600),
            )

        if response.ok:
            return response.json()

        raise UploadError(f"{response.status_code} {response.text.strip()}")

