from __future__ import annotations

import os
from functools import lru_cache
from typing import Any

from app.core.config import app_database_path, encryption_key_path, installation_id_path
from app.core.encryption import key_fingerprint, load_or_create_key
from app.core.identity import load_or_create_installation_id
from app.core.schedule import MINIMUM_SCHEDULE_MINUTES
from app.services.runner import EdgeRunner


@lru_cache(maxsize=1)
def _installation_id() -> str:
    return load_or_create_installation_id(installation_id_path())


@lru_cache(maxsize=1)
def _encryption_fingerprint() -> str:
    return key_fingerprint(load_or_create_key(encryption_key_path()))


def build_status_response(runner: EdgeRunner) -> dict[str, Any]:
    installation_id = _installation_id()
    encryption_fingerprint = _encryption_fingerprint()
    return {
        "edge_id": runner.settings.edge_id,
        "edge_instance_id": installation_id,
        "encryption_key_fingerprint": encryption_fingerprint,
        "scan_dir": os.getenv("SCAN_DIR", "/scan"),
        "scan_root": str(runner.settings.scan_root),
        "central_url": runner.settings.central_url,
        "advertised_url": runner.settings.advertised_url,
        "cron_schedule": runner.settings.cron_schedule,
        "minimum_cycle_gap_minutes": MINIMUM_SCHEDULE_MINUTES,
        "http_url": f"http://localhost:{runner.settings.http_port}",
        "settings_database": str(app_database_path()),
        "settings": runner.settings_store.snapshot(runner.settings),
        "settings_status": {
            "auth_token_configured": bool(runner.settings.auth_token.strip()),
        },
        "upload_circuit": runner.upload_client.snapshot(),
    }


def build_directory_response(runner: EdgeRunner) -> dict[str, Any]:
    return {
        "scan_dir": os.getenv("SCAN_DIR", "/scan"),
        "scan_root": str(runner.settings.scan_root),
        "directories": runner.list_directories(),
    }
