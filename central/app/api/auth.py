from __future__ import annotations

from fastapi import HTTPException

from app.core.config import Settings


def authorize_request(authorization: str | None, settings: Settings, logger) -> None:
    if authorization != f"Bearer {settings.auth_token}":
        logger.warning("auth_failure")
        raise HTTPException(status_code=401, detail="unauthorized")
