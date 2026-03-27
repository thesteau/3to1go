from __future__ import annotations

import os
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml


UPLOAD_DIR_FILENAME = ".upload_dir"
SAFE_COMPONENT_PATTERN = re.compile(r"^[A-Za-z0-9._-]+$")
VALID_SHUTDOWN_ACTIONS = {"stop", "down"}
VALID_STARTUP_ACTIONS = {"start", "up", "none"}


@dataclass(slots=True)
class DockerComposeControl:
    project_dir: Path
    compose_file: Path | None
    env_file: Path | None
    project_name: str | None
    services: list[str]
    shutdown_action: str
    startup_action: str
    command_timeout_seconds: int


@dataclass(slots=True)
class JobDefinition:
    root_path: Path
    job_name: str
    exclude_patterns: list[str]
    include_hidden: bool
    follow_symlinks: bool
    docker_compose: DockerComposeControl | None = None

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
    if not isinstance(include_hidden, bool) or not isinstance(follow_symlinks, bool):
        raise ValueError("boolean options must be true or false")

    docker_compose = _parse_docker_compose_control(payload.get("docker_compose"), directory)

    return JobDefinition(
        root_path=directory.resolve(),
        job_name=job_name,
        exclude_patterns=list(exclude),
        include_hidden=include_hidden,
        follow_symlinks=follow_symlinks,
        docker_compose=docker_compose,
    )


def job_definition_to_payload(job: JobDefinition) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "job_name": job.job_name,
        "exclude": list(job.exclude_patterns),
        "include_hidden": job.include_hidden,
        "follow_symlinks": job.follow_symlinks,
    }
    if job.docker_compose is not None:
        docker_payload: dict[str, Any] = {
            "project_dir": str(job.docker_compose.project_dir),
            "shutdown_action": job.docker_compose.shutdown_action,
            "startup_action": job.docker_compose.startup_action,
            "services": list(job.docker_compose.services),
            "command_timeout_seconds": job.docker_compose.command_timeout_seconds,
        }
        if job.docker_compose.compose_file is not None:
            docker_payload["compose_file"] = str(job.docker_compose.compose_file)
        if job.docker_compose.env_file is not None:
            docker_payload["env_file"] = str(job.docker_compose.env_file)
        if job.docker_compose.project_name is not None:
            docker_payload["project_name"] = job.docker_compose.project_name
        payload["docker_compose"] = docker_payload
    return payload


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


def _parse_docker_compose_control(raw_value: object, directory: Path) -> DockerComposeControl | None:
    if raw_value is None:
        return None

    if not isinstance(raw_value, dict):
        raise ValueError("docker_compose must be a mapping")

    project_dir_raw = raw_value.get("project_dir")
    if not isinstance(project_dir_raw, str) or not project_dir_raw.strip():
        raise ValueError("docker_compose.project_dir is required")

    project_dir = _resolve_optional_path(project_dir_raw, directory, "docker_compose.project_dir")
    if project_dir is None:
        raise ValueError("docker_compose.project_dir is required")

    compose_file = _resolve_optional_path(raw_value.get("compose_file"), project_dir, "docker_compose.compose_file")
    env_file = _resolve_optional_path(raw_value.get("env_file"), project_dir, "docker_compose.env_file")

    project_name = raw_value.get("project_name")
    if project_name is not None:
        if not isinstance(project_name, str) or not project_name.strip():
            raise ValueError("docker_compose.project_name must be a non-empty string")
        project_name = project_name.strip()

    services = raw_value.get("services") or []
    if not isinstance(services, list) or not all(isinstance(item, str) and item.strip() for item in services):
        raise ValueError("docker_compose.services must be a list of non-empty strings")
    normalized_services = [item.strip() for item in services]

    shutdown_action = str(raw_value.get("shutdown_action", "stop")).strip().lower()
    if shutdown_action not in VALID_SHUTDOWN_ACTIONS:
        raise ValueError("docker_compose.shutdown_action must be stop or down")

    default_startup_action = "up" if shutdown_action == "down" else "start"
    startup_action = str(raw_value.get("startup_action", default_startup_action)).strip().lower()
    if startup_action not in VALID_STARTUP_ACTIONS:
        raise ValueError("docker_compose.startup_action must be start, up, or none")

    timeout_raw = raw_value.get("command_timeout_seconds", 300)
    if not isinstance(timeout_raw, int) or timeout_raw < 1:
        raise ValueError("docker_compose.command_timeout_seconds must be a positive integer")

    return DockerComposeControl(
        project_dir=project_dir,
        compose_file=compose_file,
        env_file=env_file,
        project_name=project_name,
        services=normalized_services,
        shutdown_action=shutdown_action,
        startup_action=startup_action,
        command_timeout_seconds=timeout_raw,
    )


def _resolve_optional_path(raw_value: object, base_dir: Path, field_name: str) -> Path | None:
    if raw_value is None:
        return None
    if not isinstance(raw_value, str) or not raw_value.strip():
        raise ValueError(f"{field_name} must be a non-empty string")

    candidate = Path(raw_value.strip())
    if not candidate.is_absolute():
        candidate = (base_dir / candidate).resolve()
    else:
        candidate = candidate.resolve()
    return candidate
