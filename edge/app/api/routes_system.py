from __future__ import annotations

from fastapi import APIRouter, Depends, File, HTTPException, UploadFile
from fastapi.requests import Request
from fastapi.responses import HTMLResponse

from app.api.dependencies import get_runner, get_scheduler, get_start_scheduler
from app.api.models import EdgeHookCommandsInput, EdgeNtfySettingsInput, EdgeSettingsInput
from app.api.views import templates
from app.core.config import encryption_key_path
from app.core.encryption import key_as_base64, key_fingerprint, load_or_create_key
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
    payload = {**runner.settings_store.snapshot(runner.settings), **config.model_dump()}
    try:
        settings = runner.save_settings(payload)
        scheduler.reload_settings()
    except RuntimeError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok", "settings": runner.settings_store.snapshot(settings)}


@router.get("/api/ntfy")
async def get_ntfy_config(runner: EdgeRunner = Depends(get_runner)) -> dict:
    return runner.ntfy_publisher.snapshot(runner.settings)


@router.post("/api/ntfy")
async def save_ntfy_config(
    config: EdgeNtfySettingsInput,
    runner: EdgeRunner = Depends(get_runner),
    scheduler: SchedulerController = Depends(get_scheduler),
) -> dict:
    payload = {
        **runner.settings_store.snapshot(runner.settings),
        "ntfy_url": config.ntfy_url,
        "ntfy_topic": config.ntfy_topic,
        "ntfy_message_template": config.ntfy_message_template,
    }
    try:
        runner.save_settings(payload)
        scheduler.reload_settings()
    except RuntimeError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok"}


@router.post("/api/ntfy/test")
async def test_ntfy_config(
    config: EdgeNtfySettingsInput,
    runner: EdgeRunner = Depends(get_runner),
) -> dict:
    try:
        runner.ntfy_publisher.publish_test(config.model_dump())
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    except RuntimeError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc
    return {"status": "ok"}


@router.get("/api/hooks")
async def get_hooks_config(runner: EdgeRunner = Depends(get_runner)) -> dict:
    return runner.hook_manager.snapshot(
        pre_command=runner.settings.hook_pre_command,
        post_command=runner.settings.hook_post_command,
    )


@router.post("/api/hooks")
async def save_hooks_config(
    config: EdgeHookCommandsInput,
    runner: EdgeRunner = Depends(get_runner),
    scheduler: SchedulerController = Depends(get_scheduler),
) -> dict:
    payload = {
        **runner.settings_store.snapshot(runner.settings),
        "hook_pre_command": config.pre_command,
        "hook_post_command": config.post_command,
    }
    try:
        runner.save_settings(payload)
        scheduler.reload_settings()
    except RuntimeError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok"}


@router.post("/api/hooks/files")
async def upload_hook_file(
    hook_file: UploadFile = File(...),
    runner: EdgeRunner = Depends(get_runner),
) -> dict:
    try:
        content = await hook_file.read()
        saved = runner.hook_manager.save_uploaded_file(hook_file.filename or "", content)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok", "file": saved}


@router.get("/api/hooks/files/{filename}")
async def view_hook_file(
    filename: str,
    runner: EdgeRunner = Depends(get_runner),
) -> dict:
    try:
        return runner.hook_manager.read_text_file(filename)
    except FileNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc


@router.delete("/api/hooks/files/{filename}")
async def delete_hook_file(
    filename: str,
    runner: EdgeRunner = Depends(get_runner),
) -> dict:
    try:
        runner.hook_manager.delete_file(filename)
    except FileNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    return {"status": "ok"}


@router.get("/api/encryption-key")
async def get_encryption_key() -> dict:
    key = load_or_create_key(encryption_key_path())
    return {"key": key_as_base64(key), "fingerprint": key_fingerprint(key)}


@router.get("/", response_class=HTMLResponse)
async def ui(request: Request) -> HTMLResponse:
    return templates.TemplateResponse("index.html", {"request": request, "title": "RelayCentralizer Edge"})
