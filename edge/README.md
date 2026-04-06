# RelayCentralizer Edge

Edge is the scanning and upload agent. It discovers backup jobs under a scan root, fingerprints directory contents, creates `tar.zst` archives, and uploads changed jobs to Central.

It also serves a small UI for creating, editing, and deleting `.upload_dir` marker files.

## What Edge Is Responsible For

- scanning `SCAN_ROOT` for `.upload_dir` markers
- building job definitions from those marker files
- skipping unchanged jobs by comparing fingerprints
- keeping failed uploads in the spool for retry when configured
- resuming interrupted uploads by continuing from the last acknowledged byte offset
- optionally stopping and starting Docker Compose-managed directories around archive creation
- optionally pulling updated images before bringing a Compose stack back up
- uploading successful archives to Central

## Starting Edge

You can run Edge with the published image, directly with Python, or with the bundled [`docker-compose.yml`](docker-compose.yml) for local development.

Local development example:

```powershell
Copy-Item .env.example .env
```

`AUTH_TOKEN_FILE` must point to an existing file on the Edge device or inside the Edge container. Edge reads that file at startup and uses its contents to authenticate upload requests to Central.

Then start Edge:

```powershell
docker compose up --build
```

Open the UI at `http://localhost:8080/`.

## Auth Token Behavior

Edge uses filesystem-based auth configuration only.

- `AUTH_TOKEN_FILE` is required.
- The file must already exist before Edge starts.
- Edge reads the token from its own local filesystem.
- The token value must match the token file configured on Central.

Edge never reads secrets from Central's filesystem and does not depend on a shared auth folder.

## How Job Discovery Works

- Edge scans `SCAN_ROOT` for directories containing a `.upload_dir` file.
- The first `.upload_dir` found on a path becomes the backup job root.
- Nested jobs under an already selected parent are blocked.
- If a job's fingerprint has not changed since the last successful upload, Edge skips it.
- If an upload fails and `KEEP_LOCAL_PENDING=true`, the archive is kept in the spool directory and retried later.
- If an upload fails permanently, Edge marks the job as requiring manual intervention instead of retrying forever.

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
is_docker_composed: false
update_container_on_packup: false
```

Supported keys:

- `job_name`: letters, numbers, `.`, `_`, and `-`
- `exclude`: list of glob-style patterns
- `include_hidden`: include dotfiles and dot-directories
- `follow_symlinks`: follow symlinked files when building the archive
- `is_docker_composed`: set to `true` only when that directory itself contains `docker-compose.yml` or `compose.yml`
- `update_container_on_packup`: when `true`, Edge runs `docker compose pull` before `docker compose up -d`; default is `false`

## Docker Compose Behavior

When `is_docker_composed: true`, Edge checks the selected job directory itself for one of these files:

- `docker-compose.yml`
- `compose.yml`

If one is present, Edge performs the backup in this order:

1. Run `docker compose stop`.
2. Create the `tar.zst` archive.
3. If `update_container_on_packup: true`, run `docker compose pull`.
4. Run `docker compose up -d`.
5. Upload the archive to Central.

If `is_docker_composed: true` but neither Compose file is present, Edge logs the mismatch and skips the Compose operations. The backup itself still proceeds.

If `update_container_on_packup: true` while `is_docker_composed` is `false` or not set, Edge logs that contradiction and skips the update step.

## Shell Script Wrapper

Edge runs its Compose operations through the bundled scripts in [`edge/app/scripts/`](app/scripts/) so the Python quiesce logic stays small and the operational commands are easy to extend later.

## Scheduler Behavior

- `CRON_SCHEDULE` uses a 5-field cron expression.
- Edge runs one backup cycle immediately on startup.
- Scheduled runs are serialized so overlapping cycles do not run at the same time.
- The schedule may not be more frequent than every 5 minutes.
- The UI's `Run Backup Cycle Now` action goes through the same scheduler controls and also clears any manual-intervention blocks for one explicit retry attempt.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `EDGE_ID` | `edge-01` | Namespace sent to Central |
| `SCAN_ROOT` | `/scan` | Root directory Edge scans for `.upload_dir` files |
| `CENTRAL_URL` | `http://central:8000` | Base URL for Central |
| `AUTH_TOKEN_FILE` | `/run/secrets/relay_auth_token` | File containing the bearer token on the Edge host or container |
| `CRON_SCHEDULE` | `0 2 * * *` | Backup schedule inside the Edge runtime |
| `STATE_DIR` | `/data/state` | Persistent job state and retry metadata |
| `SPOOL_DIR` | `/data/spool` | Temporary archive storage before successful upload |
| `LOG_LEVEL` | `INFO` | Application log level |
| `MAX_DEPTH` | `10` | Maximum recursion depth under `SCAN_ROOT` |
| `KEEP_LOCAL_PENDING` | `true` | Keep failed-upload archives for retry |
| `UPLOAD_CHUNK_SIZE_MB` | `8` | Preferred chunk size for resumable uploads |
| `MIN_UPLOAD_CHUNK_SIZE_MB` | `1` | Minimum chunk size after adaptive backoff |
| `MAX_UPLOAD_CHUNK_SIZE_MB` | `16` | Maximum chunk size after successful transfers |
| `UPLOAD_RETRY_MAX_ATTEMPTS` | `5` | Immediate retry attempts per upload phase before the job is deferred |
| `UPLOAD_RETRY_BASE_DELAY_SECONDS` | `5` | Base delay for exponential backoff between deferred retries |
| `UPLOAD_RETRY_MAX_DELAY_SECONDS` | `300` | Maximum deferred retry delay |
| `UPLOAD_CONNECT_TIMEOUT_SECONDS` | `10` | Connect timeout per request to Central |
| `UPLOAD_READ_TIMEOUT_PADDING_SECONDS` | `30` | Read-time padding added on top of the chunk-size throughput estimate |
| `UPLOAD_MIN_THROUGHPUT_BYTES_PER_SECOND` | `262144` | Minimum expected throughput used to derive per-chunk read timeouts |
| `CIRCUIT_BREAKER_FAILURE_THRESHOLD` | `5` | Consecutive transient failures before Edge opens the Central circuit breaker |
| `CIRCUIT_BREAKER_COOLDOWN_SECONDS` | `300` | How long Edge waits before probing Central again after the circuit opens |
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

If you run Edge in Docker, mount the token file into the container yourself and keep `AUTH_TOKEN_FILE` pointed at that in-container path.

## Running Compose Support Inside The Edge Container

If you enable `is_docker_composed: true`, the Edge runtime must be able to reach the Docker CLI and the selected directory's Compose file.

That usually means mounting:

- the Docker socket
- the Compose project directory itself into the Edge container
