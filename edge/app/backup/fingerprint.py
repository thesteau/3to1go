from __future__ import annotations

import hashlib

from app.backup.filters import DiscoveredFile


def build_manifest(files: list[DiscoveredFile]) -> str:
    lines = [
        f"{file.archive_path}\t{file.size}"
        for file in sorted(files, key=lambda item: item.archive_path)
    ]
    return "\n".join(lines)


def compute_fingerprint(files: list[DiscoveredFile]) -> str:
    manifest = build_manifest(files)
    return hashlib.sha256(manifest.encode("utf-8")).hexdigest()
