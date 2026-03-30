from __future__ import annotations

from typing import Any

from app.core.schedule import MINIMUM_SCHEDULE_MINUTES
from app.services.runner import EdgeRunner


def build_directory_response(runner: EdgeRunner) -> dict[str, Any]:
    return {
        "edge_id": runner.settings.edge_id,
        "scan_root": str(runner.settings.scan_root),
        "central_url": runner.settings.central_url,
        "cron_schedule": runner.settings.cron_schedule,
        "minimum_cycle_gap_minutes": MINIMUM_SCHEDULE_MINUTES,
        "http_url": f"http://localhost:{runner.settings.http_port}",
        "directories": runner.list_directories(),
    }
