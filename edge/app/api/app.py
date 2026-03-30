from __future__ import annotations

from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles

from app.api.routes_jobs import router as jobs_router
from app.api.routes_system import router as system_router
from app.api.views import STATIC_DIR
from app.core.config import Settings, load_settings
from app.services.runner import EdgeRunner
from app.services.scheduler import SchedulerController


def create_app(settings: Settings | None = None, start_scheduler: bool = True) -> FastAPI:
    runner = EdgeRunner(settings or load_settings())
    scheduler = SchedulerController(runner)

    app = FastAPI(title="RelayCentralizer Edge", version="0.1.0")
    app.mount("/static", StaticFiles(directory=str(STATIC_DIR)), name="static")
    app.state.runner = runner
    app.state.scheduler = scheduler
    app.state.start_scheduler = start_scheduler
    app.include_router(system_router)
    app.include_router(jobs_router)

    @app.on_event("startup")
    async def on_startup() -> None:
        if start_scheduler:
            scheduler.start()

    @app.on_event("shutdown")
    async def on_shutdown() -> None:
        scheduler.stop()

    return app
