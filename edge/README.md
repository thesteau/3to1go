# RelayCentralizer Edge

RelayCentralizer Edge is the scanning and upload agent. It discovers `.upload_dir` markers under the configured scan root, fingerprints directory contents, creates `tar.zst` archives, uploads changed jobs to Central, and now includes a small HTML UI for managing job markers.

## What It Does

- Scans `SCAN_ROOT` for directories containing `.upload_dir`
- Lets users browse folders and create, edit, or delete `.upload_dir` markers from the UI
- Includes already-marked directories in the selected job list automatically
- Supports editing `job_name`, exclude patterns, hidden-file behavior, symlink behavior, and optional Docker Compose quiesce settings
- Retries pending uploads from the spool directory
- Serves a simple UI at `/` and `GET /health`

## Quick Start

1. Copy the env file:

```powershell
Copy-Item .env.example .env
```

2. Point `CENTRAL_URL` at your Central instance.

3. Start the service:

```powershell
docker compose up --build
```

4. Open the UI:

```text
http://localhost:8080/
```

## Environment

```env
EDGE_ID=edge-01
SCAN_ROOT=/scan
CENTRAL_URL=http://central:8000
AUTH_TOKEN=change-me
INTERVAL_SECONDS=3600
STATE_DIR=/data/state
SPOOL_DIR=/data/spool
LOG_LEVEL=INFO
MAX_DEPTH=10
KEEP_LOCAL_PENDING=true
HTTP_HOST=0.0.0.0
HTTP_PORT=8080
```

## Notes

- The scan root mount is writable in Docker now because the UI needs to create and delete `.upload_dir` marker files.
- If a directory already contains `.upload_dir`, it appears in the UI as an existing selected job and can be edited or deleted.
- Docker Compose quiescing is optional and requires mounting the Docker socket plus any Compose project directories the job references.

## Files

- `Dockerfile`: container image for Edge only
- `docker-compose.yml`: local compose runner for Edge only
- `.env.example`: runtime configuration example
- `app/`: Edge scheduler, archiving, upload, and UI code

## Manual Run

One-shot run:

```powershell
python -m app.main --once
```

Serve UI and scheduler:

```powershell
python -m app.main
```
