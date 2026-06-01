from __future__ import annotations

import hashlib
import random
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import requests
from requests import Response
from requests.exceptions import RequestException, Timeout

from app.core.config import Settings, encryption_key_path, installation_id_path
from app.core.encryption import key_fingerprint, load_or_create_key
from app.core.identity import load_or_create_installation_id


ProgressCallback = Callable[[str, int, int], None]


@dataclass(slots=True)
class UploadFailure(RuntimeError):
    message: str
    category: str
    retryable: bool
    status_code: int | None = None
    next_offset: int | None = None
    retry_after_seconds: int | None = None
    phase: str | None = None

    def __str__(self) -> str:
        prefix = f"{self.phase}: " if self.phase else ""
        return f"{prefix}{self.message}"


class CircuitBreaker:
    def __init__(self, failure_threshold: int, cooldown_seconds: int) -> None:
        self.failure_threshold = failure_threshold
        self.cooldown_seconds = cooldown_seconds
        self._lock = threading.Lock()
        self._consecutive_failures = 0
        self._opened_until_monotonic: float | None = None

    def before_request(self) -> None:
        with self._lock:
            if self._opened_until_monotonic is None:
                return
            now = time.monotonic()
            if now >= self._opened_until_monotonic:
                self._opened_until_monotonic = None
                return
            retry_after = max(1, int(self._opened_until_monotonic - now))
            raise UploadFailure(
                message="central circuit breaker is open",
                category="circuit_open",
                retryable=True,
                retry_after_seconds=retry_after,
            )

    def record_success(self) -> None:
        with self._lock:
            self._consecutive_failures = 0
            self._opened_until_monotonic = None

    def record_failure(self) -> None:
        with self._lock:
            self._consecutive_failures += 1
            if self._consecutive_failures >= self.failure_threshold:
                self._opened_until_monotonic = time.monotonic() + self.cooldown_seconds

    def snapshot(self) -> dict[str, int | str | None]:
        with self._lock:
            if self._opened_until_monotonic is None:
                return {
                    "state": "closed",
                    "consecutive_failures": self._consecutive_failures,
                    "cooldown_remaining_seconds": 0,
                }
            remaining = max(0, int(self._opened_until_monotonic - time.monotonic()))
            return {
                "state": "open" if remaining > 0 else "closed",
                "consecutive_failures": self._consecutive_failures,
                "cooldown_remaining_seconds": remaining,
            }


class UploadClient:
    def __init__(self, settings: Settings) -> None:
        self.central_url = settings.central_url.rstrip("/")
        self.advertised_url = settings.advertised_url.rstrip("/") if settings.advertised_url else None
        self.auth_token = settings.auth_token
        self.edge_instance_id = load_or_create_installation_id(installation_id_path())
        self.encryption_key_fingerprint = key_fingerprint(load_or_create_key(encryption_key_path()))
        self.chunk_size_bytes = settings.upload_chunk_size_bytes
        self.min_chunk_size_bytes = settings.min_upload_chunk_size_bytes
        self.max_chunk_size_bytes = max(settings.max_upload_chunk_size_bytes, settings.upload_chunk_size_bytes)
        self.max_retry_attempts = settings.upload_retry_max_attempts
        self.retry_base_delay_seconds = settings.upload_retry_base_delay_seconds
        self.retry_max_delay_seconds = settings.upload_retry_max_delay_seconds
        self.connect_timeout_seconds = settings.upload_connect_timeout_seconds
        self.read_timeout_padding_seconds = settings.upload_read_timeout_padding_seconds
        self.min_throughput_bytes_per_second = settings.upload_min_throughput_bytes_per_second
        self.session = requests.Session()
        self.circuit_breaker = CircuitBreaker(
            settings.circuit_breaker_failure_threshold,
            settings.circuit_breaker_cooldown_seconds,
        )

    def snapshot(self) -> dict[str, int | str | None]:
        return self.circuit_breaker.snapshot()

    def upload_archive(
        self,
        edge_id: str,
        job_name: str,
        fingerprint: str,
        timestamp: str,
        archive_path: Path,
        archive_sha256: str | None = None,
        upload_id: str | None = None,
        upload_offset: int = 0,
        preferred_chunk_size: int | None = None,
        progress_callback: ProgressCallback | None = None,
    ) -> dict:
        archive_size = archive_path.stat().st_size
        archive_sha256 = archive_sha256 or sha256_path(archive_path)
        idempotency_key = build_idempotency_key(
            edge_id,
            job_name,
            fingerprint,
            timestamp,
            archive_size,
            archive_sha256,
        )
        session_info = self._retry_phase(
            "initiate",
            lambda: self._initiate_session(
                edge_id=edge_id,
                job_name=job_name,
                fingerprint=fingerprint,
                timestamp=timestamp,
                archive_size=archive_size,
                archive_sha256=archive_sha256,
                idempotency_key=idempotency_key,
            ),
        )

        upload_id = session_info["upload_id"]
        offset = max(upload_offset, int(session_info.get("next_offset", 0)))
        if session_info.get("status") == "completed":
            return {
                "status": "ok",
                "stored_as": session_info.get("stored_as"),
                "pruned": int(session_info.get("pruned", 0)),
                "duplicate": bool(session_info.get("duplicate", False)),
                "upload_id": upload_id,
            }

        chunk_size = self._initial_chunk_size(
            preferred_chunk_size=preferred_chunk_size,
            recommended_chunk_size=int(session_info.get("recommended_chunk_size_bytes", self.chunk_size_bytes)),
            archive_size=archive_size,
        )
        if progress_callback is not None:
            progress_callback(upload_id, offset, chunk_size)

        success_streak = 0
        with archive_path.open("rb") as handle:
            while offset < archive_size:
                handle.seek(offset)
                chunk = handle.read(chunk_size)
                if not chunk:
                    break

                attempt = 0
                while True:
                    try:
                        response = self._send_chunk(upload_id, offset, chunk)
                    except UploadFailure as exc:
                        if exc.next_offset is not None and exc.next_offset > offset:
                            offset = exc.next_offset
                            success_streak = 0
                            if progress_callback is not None:
                                progress_callback(upload_id, offset, chunk_size)
                            break

                        attempt += 1
                        if not exc.retryable or attempt >= self.max_retry_attempts:
                            raise exc

                        reconciled = self._retry_phase(
                            "reconcile",
                            lambda: self._initiate_session(
                                edge_id=edge_id,
                                job_name=job_name,
                                fingerprint=fingerprint,
                                timestamp=timestamp,
                                archive_size=archive_size,
                                archive_sha256=archive_sha256,
                                idempotency_key=idempotency_key,
                            ),
                        )
                        offset = max(offset, int(reconciled.get("next_offset", 0)))
                        chunk_size = max(self.min_chunk_size_bytes, chunk_size // 2)
                        if progress_callback is not None:
                            progress_callback(upload_id, offset, chunk_size)
                        if offset >= archive_size:
                            break
                        handle.seek(offset)
                        chunk = handle.read(chunk_size)
                        self._sleep_before_retry(attempt, exc.retry_after_seconds)
                        continue

                    offset = int(response.get("next_offset", offset + len(chunk)))
                    success_streak += 1
                    if success_streak >= 2:
                        chunk_size = min(self.max_chunk_size_bytes, chunk_size * 2)
                        success_streak = 0
                    if progress_callback is not None:
                        progress_callback(upload_id, offset, chunk_size)
                    break

        finalize_response = self._retry_phase(
            "finalize",
            lambda: self._finalize_session(upload_id),
        )
        finalize_response["upload_id"] = upload_id
        return finalize_response

    def download_latest_snapshot(self, edge_id: str, job_name: str, destination: Path) -> dict[str, str]:
        response = self._request(
            "get",
            f"/backup/recovery/{edge_id}/{self.edge_instance_id}/{job_name}/latest",
            phase="recovery_download",
            timeout=(self.connect_timeout_seconds, self._timeout_for_bytes(self.max_chunk_size_bytes)),
            stream=True,
        )
        filename = response.headers.get("X-Relay-Snapshot-Filename") or destination.name
        destination.parent.mkdir(parents=True, exist_ok=True)
        try:
            with destination.open("wb") as handle:
                for chunk in response.iter_content(chunk_size=1024 * 1024):
                    if not chunk:
                        continue
                    handle.write(chunk)
        finally:
            response.close()
        return {"filename": filename, "path": str(destination)}

    def _retry_phase(self, phase: str, operation) -> dict:
        attempt = 0
        while True:
            try:
                return operation()
            except UploadFailure as exc:
                if exc.phase is None:
                    exc.phase = phase
                attempt += 1
                if not exc.retryable or attempt >= self.max_retry_attempts:
                    raise exc
                self._sleep_before_retry(attempt, exc.retry_after_seconds)

    def _initiate_session(
        self,
        edge_id: str,
        job_name: str,
        fingerprint: str,
        timestamp: str,
        archive_size: int,
        archive_sha256: str,
        idempotency_key: str,
    ) -> dict:
        response = self._request(
            "post",
            "/backup/uploads/initiate",
            phase="initiate",
            json={
                "edge_id": edge_id,
                "edge_instance_id": self.edge_instance_id,
                "job_name": job_name,
                "fingerprint": fingerprint,
                "timestamp": timestamp,
                "archive_format": "tar.zst",
                "archive_size_bytes": archive_size,
                "archive_sha256": archive_sha256,
                "idempotency_key": idempotency_key,
                "encryption_key_fingerprint": self.encryption_key_fingerprint,
                "advertised_url": self.advertised_url,
            },
            timeout=(self.connect_timeout_seconds, self._timeout_for_bytes(self.chunk_size_bytes)),
        )
        return response.json()

    def _send_chunk(self, upload_id: str, offset: int, chunk: bytes) -> dict:
        response = self._request(
            "put",
            f"/backup/uploads/{upload_id}/chunk",
            phase="chunk",
            params={"offset": offset},
            data=chunk,
            headers={"Content-Type": "application/octet-stream"},
            timeout=(self.connect_timeout_seconds, self._timeout_for_bytes(len(chunk))),
        )
        return response.json()

    def _finalize_session(self, upload_id: str) -> dict:
        response = self._request(
            "post",
            f"/backup/uploads/{upload_id}/finalize",
            phase="finalize",
            data=b"",
            headers={"Content-Type": "application/octet-stream"},
            timeout=(self.connect_timeout_seconds, self._timeout_for_bytes(self.chunk_size_bytes)),
        )
        return response.json()

    def _request(self, method: str, path: str, *, phase: str, **kwargs) -> Response:
        self.circuit_breaker.before_request()
        headers = {"Authorization": f"Bearer {self.auth_token}"}
        extra_headers = kwargs.pop("headers", None) or {}
        headers.update(extra_headers)
        try:
            response = self.session.request(
                method=method,
                url=f"{self.central_url}{path}",
                headers=headers,
                **kwargs,
            )
        except Timeout as exc:
            self.circuit_breaker.record_failure()
            raise UploadFailure(
                message="request timed out",
                category="network",
                retryable=True,
                phase=phase,
            ) from exc
        except RequestException as exc:
            self.circuit_breaker.record_failure()
            raise UploadFailure(
                message=str(exc),
                category="network",
                retryable=True,
                phase=phase,
            ) from exc

        if response.ok:
            self.circuit_breaker.record_success()
            return response

        failure = self._build_failure(response, phase)
        if failure.category in {"network", "server", "rate_limited"}:
            self.circuit_breaker.record_failure()
        else:
            self.circuit_breaker.record_success()
        raise failure

    def _build_failure(self, response: Response, phase: str) -> UploadFailure:
        payload = _safe_json(response)
        detail = payload.get("detail") if isinstance(payload, dict) else None
        message = _detail_message(detail) or response.text.strip() or f"http {response.status_code}"
        next_offset = _detail_next_offset(detail)
        detail_status = _detail_status(detail)
        retry_after = _retry_after_seconds(response)

        if response.status_code == 409 and detail_status == "checksum_mismatch":
            return UploadFailure(
                message=message,
                category="integrity",
                retryable=True,
                status_code=response.status_code,
                next_offset=next_offset,
                retry_after_seconds=retry_after,
                phase=phase,
            )
        if response.status_code == 409 and next_offset is not None:
            return UploadFailure(
                message=message,
                category="offset_mismatch",
                retryable=True,
                status_code=response.status_code,
                next_offset=next_offset,
                retry_after_seconds=retry_after,
                phase=phase,
            )
        if response.status_code in {408, 425, 429, 500, 502, 503, 504}:
            category = "rate_limited" if response.status_code == 429 else "server"
            return UploadFailure(
                message=message,
                category=category,
                retryable=True,
                status_code=response.status_code,
                next_offset=next_offset,
                retry_after_seconds=retry_after,
                phase=phase,
            )
        if response.status_code == 507:
            return UploadFailure(
                message=message,
                category="capacity",
                retryable=False,
                status_code=response.status_code,
                next_offset=next_offset,
                retry_after_seconds=retry_after,
                phase=phase,
            )
        if response.status_code in {400, 404, 409, 413, 422}:
            category = "validation"
            if response.status_code == 413:
                category = "too_large"
            return UploadFailure(
                message=message,
                category=category,
                retryable=False,
                status_code=response.status_code,
                next_offset=next_offset,
                retry_after_seconds=retry_after,
                phase=phase,
            )
        if response.status_code in {401, 403}:
            return UploadFailure(
                message=message,
                category="unauthorized",
                retryable=False,
                status_code=response.status_code,
                next_offset=next_offset,
                retry_after_seconds=retry_after,
                phase=phase,
            )
        return UploadFailure(
            message=message,
            category="permanent",
            retryable=False,
            status_code=response.status_code,
            next_offset=next_offset,
            retry_after_seconds=retry_after,
            phase=phase,
        )

    def _initial_chunk_size(self, *, preferred_chunk_size: int | None, recommended_chunk_size: int, archive_size: int) -> int:
        target = preferred_chunk_size or recommended_chunk_size or self.chunk_size_bytes
        target = max(self.min_chunk_size_bytes, min(self.max_chunk_size_bytes, target))
        return max(1, min(target, archive_size))

    def _timeout_for_bytes(self, payload_size: int) -> int:
        throughput_window = max(1, int(payload_size / self.min_throughput_bytes_per_second))
        return throughput_window + self.read_timeout_padding_seconds

    def _sleep_before_retry(self, attempt: int, retry_after_seconds: int | None) -> None:
        if retry_after_seconds is not None:
            delay = retry_after_seconds
        else:
            delay = min(
                self.retry_max_delay_seconds,
                self.retry_base_delay_seconds * (2 ** max(0, attempt - 1)),
            )
        jitter = min(1.0, delay * 0.2)
        time.sleep(delay + random.uniform(0, jitter))


def build_idempotency_key(
    edge_id: str,
    job_name: str,
    fingerprint: str,
    timestamp: str,
    archive_size: int,
    archive_sha256: str,
) -> str:
    digest = hashlib.sha256(
        f"{edge_id}|{job_name}|{fingerprint}|{timestamp}|{archive_size}|{archive_sha256}".encode("utf-8")
    ).hexdigest()
    return digest


def sha256_path(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while True:
            chunk = handle.read(1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
    return digest.hexdigest()


def _safe_json(response: Response) -> dict | list | None:
    try:
        return response.json()
    except ValueError:
        return None


def _detail_message(detail) -> str | None:
    if isinstance(detail, str):
        return detail
    if isinstance(detail, dict):
        message = detail.get("message")
        if isinstance(message, str) and message.strip():
            return message.strip()
        status = detail.get("status")
        next_offset = detail.get("next_offset")
        if isinstance(status, str) and next_offset is not None:
            return f"{status} next_offset={next_offset}"
        if isinstance(status, str):
            return status
    return None


def _detail_next_offset(detail) -> int | None:
    if isinstance(detail, dict):
        value = detail.get("next_offset")
        if isinstance(value, int):
            return value
    return None


def _detail_status(detail) -> str | None:
    if isinstance(detail, dict):
        value = detail.get("status")
        if isinstance(value, str):
            return value
    return None


def _retry_after_seconds(response: Response) -> int | None:
    header = response.headers.get("Retry-After")
    if header is None:
        return None
    try:
        return max(1, int(header))
    except ValueError:
        return None
