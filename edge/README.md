# RelayCentralizer Edge

RelayCentralizer Edge is the scanning and upload agent. It discovers `.upload_dir` markers under the configured scan root, fingerprints directory contents, creates `tar.zst` archives, uploads changed jobs to Central, and includes a small HTML UI for managing job markers.

## What It Does

- Scans `SCAN_ROOT` for directories containing `.upload_dir`
- Runs one backup cycle on container startup, then continues on an internal cron-style schedule
- Enforces a minimum 5-minute gap after each completed cycle so close schedules cannot pile up
- Lets users browse folders and create, edit, or delete `.upload_dir` markers from the UI
- Includes already-marked directories in the selected job list automatically
- Supports editing `job_name`, exclude patterns, hidden-file behavior, symlink behavior, and optional Docker Compose quiesce settings
- Preserves pending archives for retry when uploads fail if `KEEP_LOCAL_PENDING=true`
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
CRON_SCHEDULE=0 2 * * *
STATE_DIR=/data/state
SPOOL_DIR=/data/spool
LOG_LEVEL=INFO
MAX_DEPTH=10
KEEP_LOCAL_PENDING=true
HTTP_HOST=0.0.0.0
HTTP_PORT=8080
```

## Scheduling Notes

- `CRON_SCHEDULE` uses a standard 5-field cron expression: minute, hour, day of month, month, day of week.
- Default `0 2 * * *` means one backup every day at 02:00 inside the container timezone.
- Edge always runs one cycle immediately when the container starts.
- After any cycle completes, Edge waits at least 5 minutes before the next scheduled cycle can begin.
- Manual `Run Backup Cycle Now` requests from the UI are serialized through the same scheduler so they do not collide with scheduled work.

## Notes

- The scan root mount is writable in Docker because the UI needs to create and delete `.upload_dir` marker files.
- If a directory already contains `.upload_dir`, it appears in the UI as an existing selected job and can be edited or deleted.
- Docker Compose quiescing is optional and requires mounting the Docker socket plus any Compose project directories the job references.
- If Central is unavailable, Edge can still create the archive locally and keep it in the spool directory for a later retry.

## Files

- `Dockerfile`: container image for Edge only
- `docker-compose.yml`: local compose runner for Edge only
- `.env.example`: runtime configuration example
- `app/`: Edge scheduler, archiving, upload, and UI code
