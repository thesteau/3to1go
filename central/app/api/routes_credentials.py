from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException

from app.api.dependencies import get_settings
from app.core.config import Settings
from app.core.signing import load_or_create_issuer_keypair, mint_credential


router = APIRouter()


@router.post("/api/credentials/mint")
async def mint_edge_credential(
    ttl_days: int = 365,
    settings: Settings = Depends(get_settings),
) -> dict:
    if ttl_days < 1 or ttl_days > 3650:
        raise HTTPException(status_code=400, detail="ttl_days must be between 1 and 3650")
    try:
        private_key, _ = load_or_create_issuer_keypair(settings.issuer_key_path)
    except Exception as exc:
        raise HTTPException(status_code=500, detail="failed to load issuer key") from exc
    credential = mint_credential(private_key, ttl_days=ttl_days)
    return {"credential": credential, "ttl_days": ttl_days}
