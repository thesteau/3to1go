from __future__ import annotations

import os
import re
from collections import deque
from dataclasses import dataclass
from pathlib import Path
from typing import Any

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
    is_docker_composed: bool = False
    update_container_on_packup: bool = False

    @property
    def state_key(self) -> str:
        return str(self.root_path.resolve())


def discover_jobs(scan_root: Path, max_depth: int, logger) -> list[JobDefinition]:
    jobs: list[JobDefinition] = []
    queue: deque[tuple[Path, int]] = deque([(scan_root, 0)])

    while queue:
        directory, depth = queue.popleft()
        if depth > max_depth:
            continue

        try:
            entries = list(os.scandir(directory))
        except (FileNotFoundError, NotADirectoryError, PermissionError, OSError) as exc:
            logger.warning("skipped_missing path=%s detail=%s", directory, exc)
            continue

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
            continue

        if depth == max_depth:
            continue

        child_directories = sorted(
            (Path(entry.path) for entry in entries if entry.is_dir(follow_symlinks=False)),
            key=lambda item: item.name.lower(),
        )
        for child in child_directories:
            queue.append((child, depth + 1))

    return jobs


def load_job_definition(directory: Path, marker_path: Path, logger) -> JobDefinition | None:
    try:
        payload = read_upload_dir_payload(marker_path)
        return build_job_definition(directory=directory, payload=payload)
    except ValueError as exc:
        logger.error("invalid_upload_dir path=%s detail=%s", marker_path, exc)
        return None
    except OSError as exc:
        logger.error("invalid_upload_dir path=%s detail=%s", marker_path, exc)
        return None


def read_upload_dir_payload(marker_path: Path) -> dict[str, Any]:
    raw_content = marker_path.read_text(encoding="utf-8")
    if not raw_content.strip():
        return {}

    try:
        payload = yaml.safe_load(raw_content)
    except yaml.YAMLError as exc:
        raise ValueError(str(exc)) from exc

    if payload is None:
        return {}
    if not isinstance(payload, dict):
        raise ValueError("expected mapping")
    return payload


def build_job_definition(directory: Path, payload: dict[str, Any]) -> JobDefinition:
    job_name = str(payload.get("job_name") or directory.name).strip()
    if not SAFE_COMPONENT_PATTERN.fullmatch(job_name):
        raise ValueError("invalid job_name")

    exclude = payload.get("exclude") or []
    if not isinstance(exclude, list) or not all(isinstance(item, str) for item in exclude):
        raise ValueError("exclude must be a list of strings")

    include_hidden = payload.get("include_hidden", True)
    follow_symlinks = payload.get("follow_symlinks", False)
    is_docker_composed = payload.get("is_docker_composed", False)
    update_container_on_packup = payload.get("update_container_on_packup", False)
    if not isinstance(include_hidden, bool) or not isinstance(follow_symlinks, bool):
        raise ValueError("boolean options must be true or false")
    if not isinstance(is_docker_composed, bool):
        raise ValueError("is_docker_composed must be true or false")
    if not isinstance(update_container_on_packup, bool):
        raise ValueError("update_container_on_packup must be true or false")

    return JobDefinition(
        root_path=directory.resolve(),
        job_name=job_name,
        exclude_patterns=list(exclude),
        include_hidden=include_hidden,
        follow_symlinks=follow_symlinks,
        is_docker_composed=is_docker_composed,
        update_container_on_packup=update_container_on_packup,
    )


def job_definition_to_payload(job: JobDefinition) -> dict[str, Any]:
    return {
        "job_name": job.job_name,
        "exclude": list(job.exclude_patterns),
        "include_hidden": job.include_hidden,
        "follow_symlinks": job.follow_symlinks,
        "is_docker_composed": job.is_docker_composed,
        "update_container_on_packup": job.update_container_on_packup,
    }


def write_upload_dir(directory: Path, payload: dict[str, Any]) -> None:
    job = build_job_definition(directory.resolve(), payload)
    normalized_payload = job_definition_to_payload(job)
    marker_path = directory / UPLOAD_DIR_FILENAME
    marker_path.write_text(
        yaml.safe_dump(normalized_payload, sort_keys=False, allow_unicode=False),
        encoding="utf-8",
    )


def delete_upload_dir(directory: Path) -> None:
    marker_path = directory / UPLOAD_DIR_FILENAME
    marker_path.unlink(missing_ok=True)
