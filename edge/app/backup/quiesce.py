from __future__ import annotations

import shutil
import subprocess
from dataclasses import dataclass

from app.backup.discovery import DockerComposeControl, JobDefinition


@dataclass(slots=True)
class QuiesceContext:
    docker_compose: DockerComposeControl


class DockerComposeQuiescer:
    def __init__(self, logger) -> None:
        self.logger = logger
        self._compose_command: list[str] | None = None

    def prepare(self, job: JobDefinition) -> QuiesceContext | None:
        if job.docker_compose is None:
            return None
        self._run_action(job, job.docker_compose, job.docker_compose.shutdown_action)
        return QuiesceContext(docker_compose=job.docker_compose)

    def restore(self, job: JobDefinition, context: QuiesceContext | None) -> None:
        if context is None:
            return
        if context.docker_compose.startup_action == "none":
            self.logger.info("docker_compose_restore_skipped job_name=%s", job.job_name)
            return
        self._run_action(job, context.docker_compose, context.docker_compose.startup_action)

    def _run_action(self, job: JobDefinition, config: DockerComposeControl, action: str) -> None:
        command = self._build_command(config, action)
        self.logger.info(
            "docker_compose_action job_name=%s action=%s project_dir=%s",
            job.job_name,
            action,
            config.project_dir,
        )
        try:
            completed = subprocess.run(
                command,
                cwd=config.project_dir,
                check=True,
                capture_output=True,
                text=True,
                timeout=config.command_timeout_seconds,
            )
        except subprocess.CalledProcessError as exc:
            detail = (exc.stderr or exc.stdout or "").strip()
            raise RuntimeError(f"docker compose {action} failed: {detail}") from exc
        except subprocess.TimeoutExpired as exc:
            raise RuntimeError(f"docker compose {action} timed out") from exc

        output = (completed.stdout or "").strip()
        if output:
            self.logger.info(
                "docker_compose_result job_name=%s action=%s output=%s",
                job.job_name,
                action,
                output.replace("\n", " | "),
            )

    def _build_command(self, config: DockerComposeControl, action: str) -> list[str]:
        command = list(self._resolve_compose_command())
        if config.compose_file is not None:
            command.extend(["-f", str(config.compose_file)])
        if config.env_file is not None:
            command.extend(["--env-file", str(config.env_file)])
        if config.project_name is not None:
            command.extend(["-p", config.project_name])
        command.append(action)
        if action == "up":
            command.append("-d")
        if config.services:
            command.extend(config.services)
        return command

    def _resolve_compose_command(self) -> list[str]:
        if self._compose_command is not None:
            return self._compose_command

        docker_binary = shutil.which("docker")
        if docker_binary is not None:
            try:
                subprocess.run(
                    [docker_binary, "compose", "version"],
                    check=True,
                    capture_output=True,
                    text=True,
                    timeout=10,
                )
                self._compose_command = [docker_binary, "compose"]
                return self._compose_command
            except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
                pass

        docker_compose_binary = shutil.which("docker-compose")
        if docker_compose_binary is not None:
            self._compose_command = [docker_compose_binary]
            return self._compose_command

        raise RuntimeError("docker compose CLI is not available inside the Edge runtime")
