# RelayCentralizer Central

Central is the receiving service. It accepts backup uploads from Edge, stages them to disk, atomically commits them into local storage, and prunes older snapshots per job.

## Quick Start

1. Create a local env file:

   ```powershell
   Copy-Item .env.example .env
   ```

2. Set a real `AUTH_TOKEN` in `.env`.

3. Start the service:

   ```powershell
   docker compose up --build
   ```

4. Open the UI at `http://localhost:8000/`.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `AUTH_TOKEN` | `change-me` | Bearer token required by `POST /backup/upload` |
| `STORAGE_BACKEND` | `local` | Storage backend selector; only `local` is implemented |
| `BACKUP_ROOT` | `/backups` | Final snapshot storage location |
| `RETENTION_KEEP_LAST` | `3` | Number of snapshots to keep per `edge_id/job_name` |
| `LOG_LEVEL` | `INFO` | Application log level |
| `MAX_UPLOAD_SIZE_MB` | `2048` | Maximum accepted upload size |
| `STAGING_DIR` | `/staging` | Temporary staging area before commit |
| `HTTP_HOST` | `0.0.0.0` | Bind address |
| `HTTP_PORT` | `8000` | Listen port |

## Storage Layout

Snapshots are stored as:

```text
<BACKUP_ROOT>/<edge_id>/<job_name>/<job_name>__<timestamp>__<fingerprint>.tar.zst
```

Example:

```text
/backups/edge-01/photos/photos__2026-03-29T09-00-00Z__1a2b3c4d.tar.zst
```

Uploads are first written to `STAGING_DIR`, then moved into final storage only after the write completes successfully.

## HTTP Surface

- `GET /` - HTML status page
- `GET /api/overview` - JSON summary of storage paths, retention, and stored snapshots
- `GET /health` - health check; returns `503` if the storage backend is unavailable
- `POST /backup/upload` - multipart upload endpoint used by Edge

The upload endpoint expects:

- `Authorization: Bearer <AUTH_TOKEN>`
- form fields: `edge_id`, `job_name`, `fingerprint`, `timestamp`, `archive_format`
- file field: `archive`

## Docker Compose Notes

The provided [`docker-compose.yml`](docker-compose.yml) mounts:

- `./data/backups` -> `/backups`
- `./data/staging` -> `/staging`

That keeps uploaded archives on the host during local development.

## Running Without Docker

The app does not load `.env` automatically when run directly. Export the environment variables in your shell first, then start it from this directory:

```powershell
python -m app.main
```
