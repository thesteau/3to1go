from __future__ import annotations

import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


MAX_CERTIFICATE_FILES = 10
_CERTIFICATE_SUFFIX = ".crt"
_CERTIFICATE_UPDATE_TIMEOUT_SECONDS = 60


class CertificateManager:
    def __init__(
        self,
        storage_dir: Path,
        *,
        trust_target_dir: Path | None = None,
        update_command: str | None = None,
    ) -> None:
        self.storage_dir = storage_dir
        self.trust_target_dir = trust_target_dir or Path(
            os.getenv("RELAY_TRUST_TARGET_DIR", "/usr/local/share/ca-certificates/relay-centralizer")
        )
        self.update_command = update_command or os.getenv("RELAY_UPDATE_CA_CERTIFICATES", "update-ca-certificates")
        self.storage_dir.mkdir(parents=True, exist_ok=True)

    def snapshot(self) -> dict[str, Any]:
        return {
            "cert_dir": str(self.storage_dir),
            "max_files": MAX_CERTIFICATE_FILES,
            "files": self.list_files(),
        }

    def list_files(self) -> list[dict[str, Any]]:
        self.storage_dir.mkdir(parents=True, exist_ok=True)
        files = sorted((path for path in self.storage_dir.iterdir() if path.is_file()), key=lambda path: path.name.lower())
        return [self._file_payload(path) for path in files[:MAX_CERTIFICATE_FILES]]

    def save_uploaded_file(self, filename: str, content: bytes) -> dict[str, Any]:
        safe_name = self._sanitize_filename(filename)
        if Path(safe_name).suffix.lower() != _CERTIFICATE_SUFFIX:
            raise ValueError("only .crt certificate files are allowed")

        try:
            text = content.decode("utf-8")
        except UnicodeDecodeError as exc:
            raise ValueError("certificate must be UTF-8 PEM text") from exc

        text = self._normalize_certificate_text(text)
        if "-----BEGIN CERTIFICATE-----" not in text or "-----END CERTIFICATE-----" not in text:
            raise ValueError("certificate must be PEM encoded")

        existing = {path.name for path in self._all_files()}
        if safe_name not in existing and len(existing) >= MAX_CERTIFICATE_FILES:
            raise ValueError("only the first 10 certificate files are supported here")

        target = self.storage_dir / safe_name
        target.write_text(text, encoding="utf-8")
        self._install_trust_file(target)
        self._update_trust_store()
        return self._file_payload(target)

    def delete_file(self, filename: str) -> None:
        safe_name = self._sanitize_filename(filename)
        path = self.storage_dir / safe_name
        if not path.exists() or not path.is_file():
            raise FileNotFoundError(safe_name)
        path.unlink()
        (self.trust_target_dir / safe_name).unlink(missing_ok=True)
        self._update_trust_store()

    def _all_files(self) -> list[Path]:
        self.storage_dir.mkdir(parents=True, exist_ok=True)
        return sorted((path for path in self.storage_dir.iterdir() if path.is_file()), key=lambda path: path.name.lower())

    def _install_trust_file(self, source: Path) -> None:
        self.trust_target_dir.mkdir(parents=True, exist_ok=True)
        target = self.trust_target_dir / source.name
        target.write_text(source.read_text(encoding="utf-8"), encoding="utf-8")

    def _update_trust_store(self) -> None:
        result = subprocess.run(
            self.update_command,
            shell=True,
            capture_output=True,
            text=True,
            timeout=_CERTIFICATE_UPDATE_TIMEOUT_SECONDS,
        )
        if result.returncode != 0:
            detail = (result.stderr or result.stdout or "").strip()
            raise RuntimeError(detail or f"{self.update_command} failed")

    def _normalize_certificate_text(self, text: str) -> str:
        return text.replace("\r\n", "\n").replace("\r", "\n")

    def _sanitize_filename(self, filename: str) -> str:
        safe_name = Path(str(filename or "").strip()).name
        if not safe_name or safe_name in {".", ".."}:
            raise ValueError("filename is required")
        return safe_name

    def _file_payload(self, path: Path) -> dict[str, Any]:
        return {
            "name": path.name,
            "size_bytes": path.stat().st_size,
            "modified_at": datetime.fromtimestamp(path.stat().st_mtime, tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
