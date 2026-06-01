from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Query

from app.api.dependencies import get_runner
from app.api.models import JobConfigInput
from app.services.recovery import RecoveryError
from app.services.runner import EdgeRunner


router = APIRouter()


@router.post("/api/jobs")
async def save_job(
    config: JobConfigInput,
    runner: EdgeRunner = Depends(get_runner),
) -> dict:
    payload = {
        "job_name": config.job_name or config.relative_path.rsplit("/", 1)[-1] or runner.settings.scan_root.name,
        "exclude": [item for item in config.exclude if item],
        "include_hidden": config.include_hidden,
        "follow_symlinks": config.follow_symlinks,
        "is_docker_composed": config.is_docker_composed,
        "update_container_on_packup": config.update_container_on_packup,
    }

    try:
        job = runner.save_job(config.relative_path, payload)
    except ValueError as exc:
        detail = str(exc)
        status_code = 404 if detail == "directory not found" else 400
        raise HTTPException(status_code=status_code, detail=detail) from exc
    return {"status": "ok", "job": job}


@router.delete("/api/jobs")
async def delete_job(
    relative_path: str = Query(...),
    runner: EdgeRunner = Depends(get_runner),
) -> dict:
    try:
        runner.delete_job(relative_path)
    except ValueError as exc:
        detail = str(exc)
        status_code = 404 if detail == "directory not found" else 400
        raise HTTPException(status_code=status_code, detail=detail) from exc
    return {"status": "ok"}


@router.post("/api/jobs/force-send")
async def force_send_job(
    job_name: str = Query(..., min_length=1),
    runner: EdgeRunner = Depends(get_runner),
) -> dict:
    try:
        return runner.force_send_job(job_name)
    except ValueError as exc:
        detail = str(exc)
        if detail == "job not found":
            status_code = 404
        elif detail == "multiple jobs share that job_name":
            status_code = 409
        else:
            status_code = 400
        raise HTTPException(status_code=status_code, detail=detail) from exc


@router.post("/api/jobs/recover-latest")
async def recover_latest_job(
    relative_path: str = Query(..., min_length=1),
    runner: EdgeRunner = Depends(get_runner),
) -> dict:
    try:
        return runner.recover_latest_job(relative_path)
    except ValueError as exc:
        detail = str(exc)
        status_code = 404 if detail == "job not found" else 400
        raise HTTPException(status_code=status_code, detail=detail) from exc
    except RecoveryError as exc:
        raise HTTPException(status_code=exc.status_code, detail=str(exc)) from exc
