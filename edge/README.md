# RelayCentralizer Edge

Edge is the scanning and upload agent. It discovers backup jobs under a scan root, fingerprints directory contents, creates `tar.zst` archives, and uploads changed jobs to Central.

It also serves a small UI for creating, editing, and deleting `.upload_dir` marker files.

## What Edge Is Responsible For

- scanning `SCAN_ROOT` for `.upload_dir` markers
- building job definitions from those marker files
- skipping unchanged jobs by comparing fingerprints
- keeping failed uploads in the spool for retry when configured
- optionally quiescing a Docker Compose-managed app before archiving it
- uploading successful archives to Central

## Starting Edge

You can run Edge with the published image, directly with Python, or with the bundled [`docker-compose.yml`](docker-compose.yml) for local development.

Local development example:

```powershell
Copy-Item .env.example .env
# set AUTH_TOKEN to match Central
# set CENTRAL_URL to the Central service Edge can reach
docker compose up --build
```

Open the UI at `http://localhost:8080/`.

## How Job Discovery Works

- Edge scans `SCAN_ROOT` for directories containing a `.upload_dir` file.
- The first `.upload_dir` found on a path becomes the backup job root.
- Nested jobs under an already selected parent are blocked.
- If a job's fingerprint has not changed since the last successful upload, Edge skips it.
- If an upload fails and `KEEP_LOCAL_PENDING=true`, the archive is kept in the spool directory and retried later.

An empty `.upload_dir` is valid and uses the directory name as the default `job_name`.

## `.upload_dir` Format

Default example:

```yaml
job_name: photos
exclude:
  - '*.tmp'
  - cache/**
include_hidden: true
follow_symlinks: false
```

Supported keys:

- `job_name`: letters, numbers, `.`, `_`, and `-`
- `exclude`: list of glob-style patterns
- `include_hidden`: include dotfiles and dot-directories
- `follow_symlinks`: follow symlinked files when building the archive
- `docker_compose`: optional stop/start behavior around archive creation

## Optional `docker_compose` Quiesce Block

Only use this if Edge should stop a Docker Compose-managed app before creating the archive and start it again afterward.

```yaml
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

## Quiesce Flow For Docker Compose Jobs

When a job includes the optional `docker_compose` block, Edge performs the backup in this order:

1. Run the configured shutdown action against the target Compose project.
2. Create the `tar.zst` archive from the selected files.
3. Run the configured startup action.
4. Upload the archive to Central.

That means the application being backed up can be stopped during archive creation and brought back before the upload finishes.

Action mapping:

- `shutdown_action: stop` -> `docker compose stop`
- `shutdown_action: down` -> `docker compose down`
- `startup_action: start` -> `docker compose start`
- `startup_action: up` -> `docker compose up -d`
- `startup_action: none` -> leave the application stopped

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

## Local Compose Notes

The provided [`docker-compose.yml`](docker-compose.yml) mounts:

- `./data/scan_root` -> `/scan`
- `./data/state` -> `/data/state`
- `./data/spool` -> `/data/spool`

`/scan` is mounted read-write because the UI needs to create and delete `.upload_dir` files.

## Running Compose Quiesce Inside The Edge Container

If a job uses the optional `docker_compose` block, the Edge runtime must be able to reach the Docker CLI and the target Compose project files.

That usually means mounting:

- the Docker socket
- the relevant Compose project directories
- any referenced compose or env files

## Running Without Docker

The app does not load `.env` automatically when run directly. Export the environment variables in your shell first, then start it from this directory:

```powershell
python -m app.main
```
