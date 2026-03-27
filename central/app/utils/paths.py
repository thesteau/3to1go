from __future__ import annotations

from datetime import datetime
import re


SAFE_COMPONENT_PATTERN = re.compile(r"^[A-Za-z0-9._-]+$")


def validate_namespace_component(value: str, field_name: str) -> str:
    normalized = value.strip()
    if not normalized or not SAFE_COMPONENT_PATTERN.fullmatch(normalized):
        raise ValueError(f"{field_name} contains invalid characters")
    return normalized


def build_snapshot_filename(job_name: str, timestamp: str, fingerprint: str) -> str:
    parsed = datetime.strptime(timestamp, "%Y-%m-%dT%H:%M:%SZ")
    timestamp_component = parsed.strftime("%Y-%m-%dT%H-%M-%SZ")
    return f"{job_name}__{timestamp_component}__{fingerprint[:8]}.tar.zst"
