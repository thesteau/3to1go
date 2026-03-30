from __future__ import annotations

from fastapi import Request

from app.services.runner import EdgeRunner
from app.services.scheduler import SchedulerController


def get_runner(request: Request) -> EdgeRunner:
    return request.app.state.runner


def get_scheduler(request: Request) -> SchedulerController:
    return request.app.state.scheduler


def get_start_scheduler(request: Request) -> bool:
    return request.app.state.start_scheduler
