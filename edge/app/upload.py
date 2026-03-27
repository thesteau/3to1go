from __future__ import annotations

from pathlib import Path

import requests


class UploadError(RuntimeError):
    pass


class UploadClient:
    def __init__(self, central_url: str, auth_token: str) -> None:
        self.central_url = central_url.rstrip("/")
        self.auth_token = auth_token

    def upload_archive(
        self,
        edge_id: str,
        job_name: str,
        fingerprint: str,
        timestamp: str,
        archive_path: Path,
    ) -> dict:
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
                timeout=(10, 300),
            )

        if response.ok:
            return response.json()

        raise UploadError(f"{response.status_code} {response.text.strip()}")

