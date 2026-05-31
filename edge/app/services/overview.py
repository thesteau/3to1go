from __future__ import annotations

from typing import Any

from app.core.config import encryption_key_path, installation_id_path, settings_storage_path
from app.core.encryption import key_fingerprint, load_or_create_key
from app.core.identity import load_or_create_installation_id
from app.core.schedule import MINIMUM_SCHEDULE_MINUTES
from app.services.runner import EdgeRunner


def build_directory_response(runner: EdgeRunner) -> dict[str, Any]:
    installation_id = load_or_create_installation_id(installation_id_path())
    encryption_fingerprint = key_fingerprint(load_or_create_key(encryption_key_path()))
    return {
        "edge_id": runner.settings.edge_id,
        "edge_instance_id": installation_id,
        "encryption_key_fingerprint": encryption_fingerprint,
        "scan_root": str(runner.settings.scan_root),
        "central_url": runner.settings.central_url,
        "advertised_url": runner.settings.advertised_url,
        "cron_schedule": runner.settings.cron_schedule,
        "minimum_cycle_gap_minutes": MINIMUM_SCHEDULE_MINUTES,
        "http_url": f"http://localhost:{runner.settings.http_port}",
        "settings_path": str(settings_storage_path()),
        "settings": runner.settings_store.snapshot(runner.settings),
        "settings_status": {
            "auth_token_configured": bool(runner.settings.auth_token.strip()),
        },
        "upload_circuit": runner.upload_client.snapshot(),
        "directories": runner.list_directories(),
    }
