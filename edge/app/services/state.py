from __future__ import annotations

import json
import tempfile
import threading
from dataclasses import asdict, dataclass
from pathlib import Path


STATE_FILENAME = "edge-state.json"


@dataclass(slots=True)
class JobState:
    job_name: str | None = None
    last_successful_fingerprint: str | None = None
    last_successful_upload: str | None = None
    pending_archive: str | None = None
    pending_archive_size: int | None = None
    pending_archive_sha256: str | None = None
    pending_fingerprint: str | None = None
    pending_timestamp: str | None = None
    upload_id: str | None = None
    upload_offset: int = 0
    upload_attempt_count: int = 0
    current_chunk_size_bytes: int | None = None
    next_retry_at: str | None = None
    last_error_detail: str | None = None
    last_error_category: str | None = None
    last_upload_started_at: str | None = None
    last_upload_updated_at: str | None = None
    manual_intervention_required: bool = False
    last_status: str | None = None
    last_stored_as: str | None = None
    last_pruned: int = 0
    last_duplicate: bool = False


class StateStore:
    def __init__(self, state_dir: Path) -> None:
        self.state_dir = state_dir
        self.state_dir.mkdir(parents=True, exist_ok=True)
        self.state_path = self.state_dir / STATE_FILENAME
        self._lock = threading.RLock()
        self._data = self._load()

    def get(self, key: str) -> JobState:
        with self._lock:
            raw_value = self._data.get(key, {})
            return JobState(**raw_value)

    def set(self, key: str, job_state: JobState) -> None:
        with self._lock:
            self._data[key] = asdict(job_state)
            self._save_locked()

    def delete(self, key: str) -> None:
        with self._lock:
            if key in self._data:
                del self._data[key]
                self._save_locked()

    def referenced_pending_archives(self) -> set[str]:
        with self._lock:
            pending: set[str] = set()
            for item in self._data.values():
                archive_path = item.get("pending_archive")
                if archive_path:
                    pending.add(archive_path)
            return pending

    def snapshot(self) -> dict[str, dict]:
        with self._lock:
            return json.loads(json.dumps(self._data))

    def clear_manual_interventions(self) -> int:
        with self._lock:
            updated = 0
            for key, item in self._data.items():
                if not item.get("manual_intervention_required"):
                    continue
                item["manual_intervention_required"] = False
                item["next_retry_at"] = None
                item["last_status"] = "manual_retry_requested"
                self._data[key] = item
                updated += 1
            if updated:
                self._save_locked()
            return updated

    def clear_manual_intervention(self, key: str) -> bool:
        with self._lock:
            item = self._data.get(key)
            if not item or not item.get("manual_intervention_required"):
                return False
            item["manual_intervention_required"] = False
            item["next_retry_at"] = None
            item["last_status"] = "manual_retry_requested"
            self._data[key] = item
            self._save_locked()
            return True

    def _save_locked(self) -> None:
        with tempfile.NamedTemporaryFile(
            "w",
            encoding="utf-8",
            dir=self.state_dir,
            delete=False,
            suffix=".tmp",
        ) as handle:
            json.dump(self._data, handle, indent=2, sort_keys=True)
            handle.flush()
            temp_path = Path(handle.name)

        temp_path.replace(self.state_path)

    def _load(self) -> dict[str, dict]:
        if not self.state_path.exists():
            return {}

        try:
            return json.loads(self.state_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            return {}
