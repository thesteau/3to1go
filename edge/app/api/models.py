from __future__ import annotations

from pydantic import BaseModel, Field


class JobConfigInput(BaseModel):
    relative_path: str
    job_name: str | None = None
    exclude: list[str] = Field(default_factory=list)
    include_hidden: bool = True
    follow_symlinks: bool = False
    is_docker_composed: bool = False
    update_container_on_packup: bool = False
