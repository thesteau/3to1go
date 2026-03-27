from __future__ import annotations

import os
import re
from dataclasses import dataclass
from pathlib import Path

import yaml


UPLOAD_DIR_FILENAME = ".upload_dir"
SAFE_COMPONENT_PATTERN = re.compile(r"^[A-Za-z0-9._-]+$")


@dataclass(slots=True)
class JobDefinition:
    root_path: Path
    job_name: str
    exclude_patterns: list[str]
    include_hidden: bool
    follow_symlinks: bool

    @property
    def state_key(self) -> str:
        return str(self.root_path.resolve())


def discover_jobs(scan_root: Path, max_depth: int, logger) -> list[JobDefinition]:
    jobs: list[JobDefinition] = []

    def walk(directory: Path, depth: int) -> None:
        if depth > max_depth:
            return

        try:
            entries = list(os.scandir(directory))
        except (FileNotFoundError, NotADirectoryError, PermissionError, OSError) as exc:
            logger.warning("skipped_missing path=%s detail=%s", directory, exc)
            return

        marker = next(
            (
                Path(entry.path)
                for entry in entries
                if entry.name == UPLOAD_DIR_FILENAME and entry.is_file(follow_symlinks=False)
            ),
            None,
        )

        if marker is not None:
            job = load_job_definition(directory=directory, marker_path=marker, logger=logger)
            if job is not None:
                jobs.append(job)
                logger.info("job_discovered job_name=%s path=%s", job.job_name, job.root_path)
            return

        if depth == max_depth:
            return

        for entry in entries:
            if not entry.is_dir(follow_symlinks=False):
                continue
            walk(Path(entry.path), depth + 1)

    walk(scan_root, 0)
    return jobs


def load_job_definition(directory: Path, marker_path: Path, logger) -> JobDefinition | None:
    try:
        raw_content = marker_path.read_text(encoding="utf-8")
        payload = yaml.safe_load(raw_content) if raw_content.strip() else {}
    except yaml.YAMLError as exc:
        logger.error("invalid_upload_dir path=%s detail=%s", marker_path, exc)
        return None
    except OSError as exc:
        logger.error("invalid_upload_dir path=%s detail=%s", marker_path, exc)
        return None

    if payload is None:
        payload = {}

    if not isinstance(payload, dict):
        logger.error("invalid_upload_dir path=%s detail=expected mapping", marker_path)
        return None

    job_name = str(payload.get("job_name") or directory.name).strip()
    if not SAFE_COMPONENT_PATTERN.fullmatch(job_name):
        logger.error("invalid_upload_dir path=%s detail=invalid job_name", marker_path)
        return None

    exclude = payload.get("exclude") or []
    if not isinstance(exclude, list) or not all(isinstance(item, str) for item in exclude):
        logger.error("invalid_upload_dir path=%s detail=exclude must be a list of strings", marker_path)
        return None

    include_hidden = payload.get("include_hidden", True)
    follow_symlinks = payload.get("follow_symlinks", False)
    if not isinstance(include_hidden, bool) or not isinstance(follow_symlinks, bool):
        logger.error("invalid_upload_dir path=%s detail=boolean options must be true or false", marker_path)
        return None

    return JobDefinition(
        root_path=directory.resolve(),
        job_name=job_name,
        exclude_patterns=list(exclude),
        include_hidden=include_hidden,
        follow_symlinks=follow_symlinks,
    )

