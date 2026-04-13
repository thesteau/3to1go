from __future__ import annotations

import shutil
import subprocess
from dataclasses import dataclass
from pathlib import Path

from app.backup.discovery import JobDefinition


COMPOSE_FILENAMES = ("docker-compose.yml", "compose.yml")


@dataclass(slots=True)
class QuiesceContext:
    compose_file: Path


class DockerComposeQuiescer:
    def __init__(self, logger) -> None:
        self.logger = logger

    def prepare(self, job: JobDefinition) -> QuiesceContext | None:
        if not job.is_docker_composed:
            if job.update_container_on_packup:
                self.logger.warning(
                    "docker_compose_update_skipped job_name=%s path=%s reason=is_docker_composed_false",
                    job.job_name,
                    job.root_path,
                )
            return None

        compose_file = self._find_compose_file(job.root_path)
        if compose_file is None:
            self.logger.warning(
                "docker_compose_skipped job_name=%s path=%s reason=compose_file_missing",
                job.job_name,
                job.root_path,
            )
            if job.update_container_on_packup:
                self.logger.warning(
                    "docker_compose_update_skipped job_name=%s path=%s reason=compose_file_missing",
                    job.job_name,
                    job.root_path,
                )
            return None

        self._run_action(job, "stop", job.root_path)
        return QuiesceContext(compose_file=compose_file)

    def restore(self, job: JobDefinition, context: QuiesceContext | None) -> None:
        if context is None:
            return
        if job.update_container_on_packup:
            self._run_action(job, "pull", job.root_path)
        self._run_action(job, "up", job.root_path)

    def _find_compose_file(self, directory: Path) -> Path | None:
        for filename in COMPOSE_FILENAMES:
            candidate = directory / filename
            if candidate.is_file():
                return candidate
        return None

    def _run_action(self, job: JobDefinition, action: str, working_dir: Path) -> None:
        if shutil.which("docker") is None:
            raise RuntimeError("docker CLI is not available on this Edge host")

        self.logger.info(
            "docker_compose_action job_name=%s action=%s project_dir=%s",
            job.job_name,
            action,
            working_dir,
        )
        command = self._command_for_action(action)
        try:
            completed = subprocess.run(
                command,
                cwd=working_dir,
                check=True,
                capture_output=True,
                text=True,
                timeout=300,
            )
        except subprocess.CalledProcessError as exc:
            detail = (exc.stderr or exc.stdout or "").strip()
            raise RuntimeError(f"docker compose {action} failed: {detail}") from exc
        except subprocess.TimeoutExpired as exc:
            raise RuntimeError(f"docker compose {action} timed out") from exc

        output = ((completed.stdout or "") + (completed.stderr or "")).strip()
        if output:
            self.logger.info(
                "docker_compose_result job_name=%s action=%s output=%s",
                job.job_name,
                action,
                output.replace("\n", " | "),
            )

    def _command_for_action(self, action: str) -> list[str]:
        targets = {
            "stop": ["docker", "compose", "stop"],
            "up": ["docker", "compose", "up", "-d"],
            "pull": ["docker", "compose", "pull"],
            "down": ["docker", "compose", "down"],
            "start": ["docker", "compose", "start"],
        }
        try:
            return targets[action]
        except KeyError as exc:
            raise RuntimeError(f"unsupported docker compose action: {action}") from exc
