from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from dotenv import load_dotenv


def _env_bool(name: str, default: bool) -> bool:
    raw_value = os.getenv(name)
    if raw_value is None:
        return default
    return raw_value.strip().lower() in {"1", "true", "yes", "on"}


@dataclass(slots=True)
class Settings:
    edge_id: str
    scan_root: Path
    central_url: str
    auth_token: str
    interval_seconds: int
    state_dir: Path
    spool_dir: Path
    log_level: str
    max_depth: int
    keep_local_pending: bool


def load_settings() -> Settings:
    load_dotenv()

    return Settings(
        edge_id=os.getenv("EDGE_ID", "edge-01").strip(),
        scan_root=Path(os.getenv("SCAN_ROOT", "/scan")).resolve(),
        central_url=os.getenv("CENTRAL_URL", "http://central:8000").rstrip("/"),
        auth_token=os.getenv("AUTH_TOKEN", "change-me"),
        interval_seconds=max(1, int(os.getenv("INTERVAL_SECONDS", "3600"))),
        state_dir=Path(os.getenv("STATE_DIR", "/data/state")),
        spool_dir=Path(os.getenv("SPOOL_DIR", "/data/spool")),
        log_level=os.getenv("LOG_LEVEL", "INFO").upper(),
        max_depth=max(0, int(os.getenv("MAX_DEPTH", "10"))),
        keep_local_pending=_env_bool("KEEP_LOCAL_PENDING", True),
    )

