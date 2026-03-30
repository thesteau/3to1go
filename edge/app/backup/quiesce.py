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
        self._scripts_dir = Path(__file__).resolve().parents[2] / "scripts"

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

        self._run_script(job, "stop", job.root_path)
        return QuiesceContext(compose_file=compose_file)

    def restore(self, job: JobDefinition, context: QuiesceContext | None) -> None:
        if context is None:
            return
        if job.update_container_on_packup:
            self._run_script(job, "pull", job.root_path)
        self._run_script(job, "up", job.root_path)

    def _find_compose_file(self, directory: Path) -> Path | None:
        for filename in COMPOSE_FILENAMES:
            candidate = directory / filename
            if candidate.is_file():
                return candidate
        return None

    def _run_script(self, job: JobDefinition, action: str, working_dir: Path) -> None:
        script_path = self._script_for_action(action)
        if shutil.which("docker") is None:
            raise RuntimeError("docker CLI is not available inside the Edge runtime")

        self.logger.info(
            "docker_compose_action job_name=%s action=%s project_dir=%s via=script",
            job.job_name,
            action,
            working_dir,
        )
        try:
            completed = subprocess.run(
                ["/bin/sh", str(script_path)],
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

    def _script_for_action(self, action: str) -> Path:
        targets = {
            "stop": self._scripts_dir / "docker_compose_stop.sh",
            "up": self._scripts_dir / "docker_compose_up.sh",
            "pull": self._scripts_dir / "docker_compose_pull.sh",
            "down": self._scripts_dir / "docker_compose_down.sh",
            "start": self._scripts_dir / "docker_compose_start.sh",
        }
        try:
            return targets[action]
        except KeyError as exc:
            raise RuntimeError(f"unsupported docker compose action: {action}") from exc
