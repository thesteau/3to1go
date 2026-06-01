from __future__ import annotations

from fastapi import APIRouter, Depends, File, HTTPException, UploadFile
from fastapi.requests import Request

from app.api.dependencies import get_hook_manager, get_ntfy_publisher, get_settings, get_settings_store
from app.api.models import CentralHookCommandsInput, CentralNtfySettingsInput
from app.core.config import Settings
from app.services.hooks import HookManager
from app.services.ntfy import NtfyPublisher
from app.services.settings_store import SettingsStore


router = APIRouter()


@router.get("/api/ntfy")
async def get_ntfy_config(
    settings: Settings = Depends(get_settings),
    ntfy_publisher: NtfyPublisher = Depends(get_ntfy_publisher),
) -> dict:
    return ntfy_publisher.snapshot(settings)


@router.post("/api/ntfy")
async def save_ntfy_config(
    config: CentralNtfySettingsInput,
    request: Request,
    settings: Settings = Depends(get_settings),
    settings_store: SettingsStore = Depends(get_settings_store),
) -> dict:
    payload = {
        **settings_store.snapshot(settings),
        "ntfy_url": config.ntfy_url,
        "ntfy_topic": config.ntfy_topic,
        "ntfy_message_template": config.ntfy_message_template,
        "ntfy_match_edge_id": config.ntfy_match_edge_id,
        "ntfy_match_edge_instance_id": config.ntfy_match_edge_instance_id,
        "ntfy_match_source": config.ntfy_match_source,
    }
    try:
        saved = settings_store.save(payload)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    request.app.state.apply_settings(saved)
    return {"status": "ok", "ntfy": payload}


@router.post("/api/ntfy/test")
async def test_ntfy_config(
    config: CentralNtfySettingsInput,
    ntfy_publisher: NtfyPublisher = Depends(get_ntfy_publisher),
) -> dict:
    try:
        ntfy_publisher.publish_test(config.model_dump())
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    except RuntimeError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc
    return {"status": "ok"}


@router.get("/api/hooks")
async def get_hooks_config(
    settings: Settings = Depends(get_settings),
    hook_manager: HookManager = Depends(get_hook_manager),
) -> dict:
    return hook_manager.snapshot(pre_command=settings.hook_pre_command, post_command=settings.hook_post_command)


@router.post("/api/hooks")
async def save_hooks_config(
    config: CentralHookCommandsInput,
    request: Request,
    settings: Settings = Depends(get_settings),
    settings_store: SettingsStore = Depends(get_settings_store),
) -> dict:
    payload = {
        **settings_store.snapshot(settings),
        "hook_pre_command": config.pre_command,
        "hook_post_command": config.post_command,
    }
    try:
        saved = settings_store.save(payload)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    request.app.state.apply_settings(saved)
    return {"status": "ok", "hooks": {"pre_command": config.pre_command, "post_command": config.post_command}}


@router.post("/api/hooks/files")
async def upload_hook_file(
    hook_file: UploadFile = File(...),
    hook_manager: HookManager = Depends(get_hook_manager),
) -> dict:
    try:
        content = await hook_file.read()
        saved = hook_manager.save_uploaded_file(hook_file.filename or "", content)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return {"status": "ok", "file": saved}


@router.get("/api/hooks/files/{filename}")
async def view_hook_file(
    filename: str,
    hook_manager: HookManager = Depends(get_hook_manager),
) -> dict:
    try:
        return hook_manager.read_text_file(filename)
    except FileNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc


@router.delete("/api/hooks/files/{filename}")
async def delete_hook_file(
    filename: str,
    hook_manager: HookManager = Depends(get_hook_manager),
) -> dict:
    try:
        hook_manager.delete_file(filename)
    except FileNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    return {"status": "ok"}
