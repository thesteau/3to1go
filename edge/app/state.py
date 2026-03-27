from __future__ import annotations

import json
import tempfile
from dataclasses import asdict, dataclass
from pathlib import Path


STATE_FILENAME = "edge-state.json"


@dataclass(slots=True)
class JobState:
    job_name: str | None = None
    last_successful_fingerprint: str | None = None
    last_successful_upload: str | None = None
    pending_archive: str | None = None
    pending_fingerprint: str | None = None
    pending_timestamp: str | None = None
    last_status: str | None = None


class StateStore:
    def __init__(self, state_dir: Path) -> None:
        self.state_dir = state_dir
        self.state_dir.mkdir(parents=True, exist_ok=True)
        self.state_path = self.state_dir / STATE_FILENAME
        self._data = self._load()

    def get(self, key: str) -> JobState:
        raw_value = self._data.get(key, {})
        return JobState(**raw_value)

    def set(self, key: str, job_state: JobState) -> None:
        self._data[key] = asdict(job_state)
        self.save()

    def save(self) -> None:
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

    def referenced_pending_archives(self) -> set[str]:
        pending: set[str] = set()
        for item in self._data.values():
            archive_path = item.get("pending_archive")
            if archive_path:
                pending.add(archive_path)
        return pending

    def _load(self) -> dict[str, dict]:
        if not self.state_path.exists():
            return {}

        try:
            return json.loads(self.state_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            return {}

