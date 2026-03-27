from __future__ import annotations

from pathlib import Path

from fastapi import FastAPI, HTTPException, Query
from fastapi.requests import Request
from fastapi.responses import HTMLResponse
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates

from app.api.models import JobConfigInput
from app.core.config import Settings, load_settings
from app.services.runner import EdgeRunner, build_directory_response
from app.services.scheduler import SchedulerController


API_DIR = Path(__file__).resolve().parent
TEMPLATES = Jinja2Templates(directory=str(API_DIR / "templates"))


def create_app(settings: Settings | None = None, dry_run: bool = False, start_scheduler: bool = True) -> FastAPI:
    runner = EdgeRunner(settings or load_settings(), dry_run=dry_run)
    scheduler = SchedulerController(runner)

    app = FastAPI(title="RelayCentralizer Edge", version="0.1.0")
    app.mount("/static", StaticFiles(directory=str(API_DIR / "static")), name="static")
    app.state.runner = runner
    app.state.scheduler = scheduler

    @app.on_event("startup")
    async def on_startup() -> None:
        if start_scheduler:
            scheduler.start()

    @app.on_event("shutdown")
    async def on_shutdown() -> None:
        scheduler.stop()

    @app.get("/health")
    async def health() -> dict:
        return {"status": "ok"}

    @app.get("/api/directories")
    async def list_directories() -> dict:
        return build_directory_response(runner)

    @app.post("/api/jobs")
    async def save_job(config: JobConfigInput) -> dict:
        payload = {
            "job_name": config.job_name or config.relative_path.rsplit("/", 1)[-1] or runner.settings.scan_root.name,
            "exclude": [item for item in config.exclude if item],
            "include_hidden": config.include_hidden,
            "follow_symlinks": config.follow_symlinks,
        }
        if config.docker_compose is not None:
            payload["docker_compose"] = config.docker_compose.model_dump(exclude_none=True)

        try:
            job = runner.save_job(config.relative_path, payload)
        except ValueError as exc:
            detail = str(exc)
            status_code = 404 if detail == "directory not found" else 400
            raise HTTPException(status_code=status_code, detail=detail) from exc
        return {"status": "ok", "job": job}

    @app.delete("/api/jobs")
    async def delete_job(relative_path: str = Query(...)) -> dict:
        try:
            runner.delete_job(relative_path)
        except ValueError as exc:
            detail = str(exc)
            status_code = 404 if detail == "directory not found" else 400
            raise HTTPException(status_code=status_code, detail=detail) from exc
        return {"status": "ok"}

    @app.post("/api/run-now")
    async def run_now() -> dict:
        return {"status": "started" if runner.run_cycle() else "already_running"}

    @app.get("/", response_class=HTMLResponse)
    async def ui(request: Request) -> HTMLResponse:
        return TEMPLATES.TemplateResponse("index.html", {"request": request, "title": "RelayCentralizer Edge"})

    return app
