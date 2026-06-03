from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Request, Response
from pydantic import BaseModel, Field

from app.services.settings_store import SettingsStore
from app.services.scheduler import SchedulerController
from app.services.user_store import SESSION_COOKIE, UserStore


router = APIRouter()


class LoginInput(BaseModel):
    username: str
    password: str


class PasswordChangeInput(BaseModel):
    current_password: str = ""
    new_password: str = Field(min_length=4)


class UserInput(BaseModel):
    username: str
    password: str = Field(min_length=4)
    is_admin: bool = False


class UserUpdateInput(BaseModel):
    username: str | None = None
    password: str | None = None
    is_admin: bool | None = None


def get_user_store(request: Request) -> UserStore:
    return request.app.state.user_store


def get_settings_store(request: Request) -> SettingsStore:
    return request.app.state.runner.settings_store


def get_scheduler(request: Request) -> SchedulerController:
    return request.app.state.scheduler


def current_user(request: Request) -> dict:
    user = getattr(request.state, "current_user", None)
    if not user:
        raise HTTPException(status_code=401, detail="login required")
    return user


def admin_user(user: dict = Depends(current_user)) -> dict:
    if not user.get("is_admin"):
        raise HTTPException(status_code=403, detail="admin required")
    return user


@router.get("/api/session/me")
async def session_me(request: Request, user_store: UserStore = Depends(get_user_store)) -> dict:
    user = user_store.user_for_session(request.cookies.get(SESSION_COOKIE))
    return {"authenticated": bool(user), "user": user}


@router.post("/api/session/login")
async def login(payload: LoginInput, response: Response, user_store: UserStore = Depends(get_user_store)) -> dict:
    user = user_store.authenticate(payload.username, payload.password)
    if user is None:
        raise HTTPException(status_code=401, detail="invalid username or password")
    token = user_store.create_session(user["id"])
    response.set_cookie(SESSION_COOKIE, token, httponly=True, samesite="lax", max_age=7 * 24 * 60 * 60)
    return {"status": "ok", "user": user}


@router.post("/api/session/logout")
async def logout(request: Request, response: Response, user_store: UserStore = Depends(get_user_store)) -> dict:
    user_store.delete_session(request.cookies.get(SESSION_COOKIE, ""))
    response.delete_cookie(SESSION_COOKIE)
    return {"status": "ok"}


@router.post("/api/session/change-password")
async def change_password(
    payload: PasswordChangeInput,
    user: dict = Depends(current_user),
    user_store: UserStore = Depends(get_user_store),
) -> dict:
    try:
        updated = user_store.change_password(
            user["id"],
            payload.current_password,
            payload.new_password,
            require_current=not user.get("must_change_password"),
        )
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok", "user": updated}


@router.get("/api/users")
async def list_users(user: dict = Depends(current_user), user_store: UserStore = Depends(get_user_store)) -> dict:
    users = user_store.list_users() if user.get("is_admin") else [user]
    return {"users": users}


@router.post("/api/users")
async def create_user(
    payload: UserInput,
    _admin: dict = Depends(admin_user),
    user_store: UserStore = Depends(get_user_store),
) -> dict:
    try:
        user = user_store.create_user(payload.username, payload.password, payload.is_admin)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok", "user": user}


@router.put("/api/users/{user_id}")
async def update_user(
    user_id: int,
    payload: UserUpdateInput,
    user: dict = Depends(current_user),
    user_store: UserStore = Depends(get_user_store),
) -> dict:
    if not user.get("is_admin") and user["id"] != user_id:
        raise HTTPException(status_code=403, detail="admin required")
    if not user.get("is_admin") and payload.is_admin is not None:
        raise HTTPException(status_code=403, detail="admin required")
    if user["id"] == user_id and payload.is_admin is not None:
        raise HTTPException(status_code=400, detail="you cannot change your own admin access")
    if payload.password and (not user.get("is_admin") or user["id"] == user_id):
        raise HTTPException(status_code=403, detail="use change password")
    try:
        updated = user_store.update_user(
            user_id,
            username=payload.username,
            password=payload.password if user.get("is_admin") else None,
            is_admin=payload.is_admin if user.get("is_admin") else None,
            must_change_password=False if payload.password else None,
        )
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok", "user": updated}


@router.delete("/api/users/{user_id}")
async def delete_user(
    user_id: int,
    _admin: dict = Depends(admin_user),
    user_store: UserStore = Depends(get_user_store),
) -> dict:
    try:
        user_store.delete_user(user_id)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok"}


@router.get("/api/migration")
async def migration_status(
    _admin: dict = Depends(admin_user),
    settings_store: SettingsStore = Depends(get_settings_store),
) -> dict:
    return settings_store.migration_status()


@router.post("/api/migration/run")
async def run_migration(
    request: Request,
    _admin: dict = Depends(admin_user),
    settings_store: SettingsStore = Depends(get_settings_store),
    scheduler: SchedulerController = Depends(get_scheduler),
) -> dict:
    result = settings_store.run_migration()
    runner = request.app.state.runner
    runner.save_settings(runner.settings_store.snapshot(runner.settings))
    scheduler.reload_settings()
    return result
