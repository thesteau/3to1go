# RelayCentralizer Central

RelayCentralizer Central is the receiver service. It exposes an HTTP upload API and a small HTML landing page, stages archives to disk, atomically commits them into local storage, and prunes older snapshots per job.

## What It Does

- Accepts `POST /backup/upload` with bearer-token auth
- Stores snapshots under `<BACKUP_ROOT>/<edge_id>/<job_name>/`
- Keeps the latest `RETENTION_KEEP_LAST` snapshots per job
- Serves a simple UI at `/` showing current storage paths and stored snapshots
- Exposes `GET /health` for health checks

## Quick Start

1. Copy the env file:

```powershell
Copy-Item .env.example .env
```

2. Start the service:

```powershell
docker compose up --build
```

3. Open the UI:

```text
http://localhost:8000/
```

## Environment

```env
AUTH_TOKEN=change-me
STORAGE_BACKEND=local
BACKUP_ROOT=/backups
RETENTION_KEEP_LAST=3
LOG_LEVEL=INFO
MAX_UPLOAD_SIZE_MB=2048
STAGING_DIR=/staging
HTTP_HOST=0.0.0.0
HTTP_PORT=8000
```

## Files

- `Dockerfile`: container image for Central only
- `docker-compose.yml`: local compose runner for Central only
- `.env.example`: runtime configuration example
- `app/`: FastAPI application

## Manual Run

```powershell
python -m app.main
```
