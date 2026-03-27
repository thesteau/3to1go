from __future__ import annotations

from pydantic import BaseModel, Field


class DockerComposeInput(BaseModel):
    project_dir: str
    compose_file: str | None = None
    env_file: str | None = None
    project_name: str | None = None
    services: list[str] = Field(default_factory=list)
    shutdown_action: str = "stop"
    startup_action: str | None = None
    command_timeout_seconds: int = 300


class JobConfigInput(BaseModel):
    relative_path: str
    job_name: str | None = None
    exclude: list[str] = Field(default_factory=list)
    include_hidden: bool = True
    follow_symlinks: bool = False
    docker_compose: DockerComposeInput | None = None
