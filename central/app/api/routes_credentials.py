from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException

from app.api.dependencies import get_credential_store, get_settings, get_snapshot_index
from app.api.routes_admin import admin_user
from app.core.config import Settings
from app.core.signing import load_or_create_issuer_keypair
from app.index.base import SnapshotIndexBackend
from app.services.credential_store import CredentialStore


router = APIRouter()


@router.post("/api/credentials/mint")
async def mint_edge_credential(
    ttl_days: int = 365,
    settings: Settings = Depends(get_settings),
    credential_store: CredentialStore = Depends(get_credential_store),
    _admin: dict = Depends(admin_user),
) -> dict:
    if ttl_days < 1 or ttl_days > 3650:
        raise HTTPException(status_code=400, detail="ttl_days must be between 1 and 3650")
    try:
        private_key, _ = load_or_create_issuer_keypair(settings.issuer_key_path)
    except Exception as exc:
        raise HTTPException(status_code=500, detail="failed to load issuer key") from exc
    credential = credential_store.mint(private_key, ttl_days=ttl_days)
    return {"credential": credential, "ttl_days": ttl_days}


@router.delete("/api/credentials/instances/{edge_id}/{edge_instance_id}")
async def revoke_instance_credential(
    edge_id: str,
    edge_instance_id: str,
    snapshot_index: SnapshotIndexBackend = Depends(get_snapshot_index),
    credential_store: CredentialStore = Depends(get_credential_store),
    _admin: dict = Depends(admin_user),
) -> dict:
    registration = snapshot_index.get_edge_registration(edge_id, edge_instance_id)
    if registration is None:
        raise HTTPException(status_code=404, detail="instance not found")

    token_hash = str(registration.get("credential_hash") or "")
    if not token_hash:
        raise HTTPException(
            status_code=409,
            detail="instance has not used a database-backed credential yet",
        )

    affected_registrations = [
        item
        for item in snapshot_index.list_edge_registrations()
        if item.get("credential_hash") == token_hash
    ]
    affected = [
        {
            "edge_id": item.get("edge_id"),
            "edge_instance_id": item.get("edge_instance_id"),
        }
        for item in affected_registrations
    ]
    revoked = credential_store.revoke(token_hash)
    for item in affected_registrations:
        updated = dict(item)
        updated["credential_hash"] = None
        snapshot_index.upsert_edge_registration(updated)
    return {"status": "revoked", "revoked_rows": revoked, "affected_instances": affected}
