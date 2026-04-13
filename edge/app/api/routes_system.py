from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException
from fastapi.requests import Request
from fastapi.responses import HTMLResponse

from app.api.dependencies import get_runner, get_scheduler, get_start_scheduler
from app.api.models import EdgeSettingsInput
from app.api.views import templates
from app.services.overview import build_directory_response
from app.services.runner import EdgeRunner
from app.services.scheduler import SchedulerController


router = APIRouter()


@router.get("/health")
async def health() -> dict:
    return {"status": "ok"}


@router.get("/api/directories")
async def list_directories(
    runner: EdgeRunner = Depends(get_runner),
    scheduler: SchedulerController = Depends(get_scheduler),
) -> dict:
    response = build_directory_response(runner)
    response["scheduler"] = scheduler.snapshot()
    return response


@router.post("/api/run-now")
async def run_now(
    runner: EdgeRunner = Depends(get_runner),
    scheduler: SchedulerController = Depends(get_scheduler),
    start_scheduler: bool = Depends(get_start_scheduler),
) -> dict:
    cleared = runner.clear_manual_interventions()
    if start_scheduler:
        return {"status": scheduler.request_run_now(), "manual_retries_cleared": cleared}
    return {
        "status": "started" if runner.run_cycle() else "already_running",
        "manual_retries_cleared": cleared,
    }


@router.post("/api/settings")
async def save_settings(
    config: EdgeSettingsInput,
    runner: EdgeRunner = Depends(get_runner),
    scheduler: SchedulerController = Depends(get_scheduler),
) -> dict:
    payload = config.model_dump()
    try:
        settings = runner.save_settings(payload)
        scheduler.reload_settings()
    except RuntimeError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok", "settings": build_directory_response(runner)["settings"]}


@router.get("/", response_class=HTMLResponse)
async def ui(request: Request) -> HTMLResponse:
    return templates.TemplateResponse("index.html", {"request": request, "title": "RelayCentralizer Edge"})
