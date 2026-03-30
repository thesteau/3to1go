# RelayCentralizer Edge

Edge is the scanning and upload agent. It discovers backup jobs under a scan root, fingerprints directory contents, creates `tar.zst` archives, and uploads changed jobs to Central.

It also serves a small UI for creating, editing, and deleting `.upload_dir` marker files.

## Quick Start

1. Create a local env file:

   ```powershell
   Copy-Item .env.example .env
   ```

2. Update `AUTH_TOKEN` to match Central and `CENTRAL_URL` to point at the Central service Edge can reach.

3. Start the service:

   ```powershell
   docker compose up --build
   ```

4. Open the UI at `http://localhost:8080/`.

## How Job Discovery Works

- Edge scans `SCAN_ROOT` for directories containing a `.upload_dir` file.
- The first `.upload_dir` found on a path becomes the backup job root.
- Nested jobs under an already selected parent are blocked.
- If a job's fingerprint has not changed since the last successful upload, Edge skips it.
- If an upload fails and `KEEP_LOCAL_PENDING=true`, the archive is kept in the spool directory and retried later.

An empty `.upload_dir` is valid and uses the directory name as the default `job_name`.

## `.upload_dir` Format

Example:

```yaml
job_name: photos
exclude:
  - '*.tmp'
  - cache/**
include_hidden: true
follow_symlinks: false
docker_compose:
  project_dir: /srv/stacks/photos
  compose_file: docker-compose.yml
  env_file: .env
  project_name: photos
  services:
    - app
    - worker
  shutdown_action: stop
  startup_action: start
  command_timeout_seconds: 300
```

Supported keys:

- `job_name`: letters, numbers, `.`, `_`, and `-`
- `exclude`: list of glob-style patterns
- `include_hidden`: include dotfiles and dot-directories
- `follow_symlinks`: follow symlinked files when building the archive
- `docker_compose`: optional stop/start behavior around archive creation

## Scheduler Behavior

- `CRON_SCHEDULE` uses a 5-field cron expression.
- Edge runs one backup cycle immediately on startup.
- Scheduled runs are serialized so overlapping cycles do not run at the same time.
- The schedule may not be more frequent than every 5 minutes.
- The UI's `Run Backup Cycle Now` action goes through the same scheduler controls.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `EDGE_ID` | `edge-01` | Namespace sent to Central |
| `SCAN_ROOT` | `/scan` | Root directory Edge scans for `.upload_dir` files |
| `CENTRAL_URL` | `http://central:8000` | Base URL for Central |
| `AUTH_TOKEN` | `change-me` | Bearer token used for uploads |
| `CRON_SCHEDULE` | `0 2 * * *` | Backup schedule inside the Edge runtime |
| `STATE_DIR` | `/data/state` | Persistent job state and retry metadata |
| `SPOOL_DIR` | `/data/spool` | Temporary archive storage before successful upload |
| `LOG_LEVEL` | `INFO` | Application log level |
| `MAX_DEPTH` | `10` | Maximum recursion depth under `SCAN_ROOT` |
| `KEEP_LOCAL_PENDING` | `true` | Keep failed-upload archives for retry |
| `HTTP_HOST` | `0.0.0.0` | Bind address |
| `HTTP_PORT` | `8080` | Listen port |

## HTTP Surface

- `GET /` - HTML job-management UI
- `GET /health` - health check
- `GET /api/directories` - scan-root view, job configs, job state, and scheduler status
- `POST /api/jobs` - create or update a `.upload_dir`
- `DELETE /api/jobs?relative_path=...` - remove a `.upload_dir`
- `POST /api/run-now` - request an immediate backup cycle

## Docker Compose Notes

The provided [`docker-compose.yml`](docker-compose.yml) mounts:

- `./data/scan_root` -> `/scan`
- `./data/state` -> `/data/state`
- `./data/spool` -> `/data/spool`

`/scan` is mounted read-write because the UI needs to create and delete `.upload_dir` files.

## Docker Quiesce Support

If a job uses the optional `docker_compose` block, Edge can stop services before archiving and start them again afterward.

For that to work inside the container, you need to mount:

- the Docker socket
- the relevant Compose project directories
- any referenced compose or env files

The Edge image includes `docker` and `docker-compose` so those commands are available inside the runtime.

## Running Without Docker

The app does not load `.env` automatically when run directly. Export the environment variables in your shell first, then start it from this directory:

```powershell
python -m app.main
```
