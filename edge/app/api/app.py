from __future__ import annotations

from fastapi import FastAPI
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles

from app.api.routes_admin import router as admin_router
from app.api.routes_jobs import router as jobs_router
from app.api.routes_system import router as system_router
from app.api.views import STATIC_DIR
from app.core.config import Settings, load_settings
from app.services.runner import EdgeRunner
from app.services.scheduler import SchedulerController
from app.services.user_store import SESSION_COOKIE, UserStore


def create_app(settings: Settings | None = None, start_scheduler: bool = True) -> FastAPI:
    provided_settings = settings is not None
    runner = EdgeRunner(settings or load_settings())
    scheduler = SchedulerController(runner)

    app = FastAPI(title="RelayCentralizer Edge", version="0.1.0")
    app.mount("/static", StaticFiles(directory=str(STATIC_DIR)), name="static")
    app.state.runner = runner
    app.state.scheduler = scheduler
    app.state.start_scheduler = start_scheduler
    app.state.user_store = UserStore(sqlite_path=runner.settings.state_dir.parent / "edge-users.db" if provided_settings else None)
    app.include_router(admin_router)
    app.include_router(system_router)
    app.include_router(jobs_router)

    @app.middleware("http")
    async def require_web_session(request, call_next):
        path = request.url.path
        user = app.state.user_store.user_for_session(request.cookies.get(SESSION_COOKIE))
        request.state.current_user = user

        if _is_public_path(path):
            return await call_next(request)
        if path.startswith("/api/") and user is None:
            return JSONResponse(status_code=401, content={"detail": "login required"})
        if path.startswith("/api/") and user.get("must_change_password"):
            return JSONResponse(status_code=403, content={"detail": "password change required"})
        return await call_next(request)

    @app.on_event("startup")
    async def on_startup() -> None:
        if start_scheduler:
            scheduler.start()

    @app.on_event("shutdown")
    async def on_shutdown() -> None:
        scheduler.stop()

    return app


def _is_public_path(path: str) -> bool:
    return (
        path == "/"
        or path.startswith("/static/")
        or path == "/health"
        or path in {
            "/api/session/me",
            "/api/session/login",
            "/api/session/logout",
            "/api/session/change-password",
        }
    )
