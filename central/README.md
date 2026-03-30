# RelayCentralizer Central

Central is the receiving service. It accepts backup uploads from Edge, stages them to disk, atomically commits them into local storage, and prunes older snapshots per job namespace.

## What Central Is Responsible For

- authenticating uploads from Edge with the configured bearer token
- staging incoming archives before commit
- storing snapshots under `<BACKUP_ROOT>/<edge_id>/<job_name>/`
- pruning older snapshots according to `RETENTION_KEEP_LAST`
- exposing a small UI and JSON overview of stored backups

## Storage Scope

Central intentionally stores backups in local filesystem storage rooted at `BACKUP_ROOT`.

If you want a second copy in S3, Google Drive, Dropbox, or another external system, the recommended approach is to sync `BACKUP_ROOT` outward with a separate service, scheduled task, or host-level script.

That keeps Central focused on receiving, committing, and retaining backups without mixing in cloud-provider-specific sync logic.

## Starting Central

You can run Central with the published image, directly with Python, or with the bundled [`docker-compose.yml`](docker-compose.yml) for local development.

Local development example:

```powershell
Copy-Item .env.example .env
```

`AUTH_TOKEN_FILE` points to Central's local token file path. If the file already exists, Central reuses it. If it is missing, Central creates it once at startup and keeps using that same file on later restarts.

Then start Central:

```powershell
docker compose up --build
```

Open the UI at `http://localhost:8000/`.

## Auth Token Behavior

Central uses filesystem-based auth configuration only.

- `AUTH_TOKEN_FILE` is required.
- If the file already exists, Central reuses it.
- If the file is missing, Central creates it at startup.
- Central only manages its own local token file.
- Central does not write token files for Edge devices.

Each Edge device must be configured with its own local token file containing the same token value.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `AUTH_TOKEN_FILE` | `/run/secrets/relay_auth_token` | File containing the bearer token for upload authentication; created by Central if missing |
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

- `Authorization: Bearer <token from AUTH_TOKEN_FILE>`
- form fields: `edge_id`, `job_name`, `fingerprint`, `timestamp`, `archive_format`
- file field: `archive`

## Local Compose Notes

The provided [`docker-compose.yml`](docker-compose.yml) mounts:

- `./data/backups` -> `/backups`
- `./data/staging` -> `/staging`

If you run Central in Docker and want bootstrap behavior, mount a writable path so Central can create `/run/secrets/relay_auth_token` when it is missing.
