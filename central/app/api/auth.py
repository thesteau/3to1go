from __future__ import annotations

from fastapi import HTTPException

from app.core.config import Settings
from app.core.signing import public_key_from_bytes, verify_credential


def authorize_request(authorization: str | None, settings: Settings, logger) -> None:
    if not authorization or not authorization.startswith("Bearer "):
        logger.warning("auth_failure")
        raise HTTPException(status_code=401, detail="unauthorized")
    token = authorization[len("Bearer "):]
    try:
        public_key = public_key_from_bytes(settings.issuer_public_key_bytes)
        verify_credential(token, public_key, settings.revoked_credentials)
    except ValueError:
        logger.warning("auth_failure")
        raise HTTPException(status_code=401, detail="unauthorized")
