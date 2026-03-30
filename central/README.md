# RelayCentralizer Central

Central is the receiving service. It accepts backup uploads from Edge, stages them to disk, atomically commits them into local storage, and prunes older snapshots per job namespace.

## What Central Is Responsible For

- authenticating uploads from Edge with a shared bearer token
- staging incoming archives before commit
- storing snapshots under `<BACKUP_ROOT>/<edge_id>/<job_name>/`
- pruning older snapshots according to `RETENTION_KEEP_LAST`
- exposing a small UI and JSON overview of stored backups

## Starting Central

You can run Central with the published image, directly with Python, or with the bundled [`docker-compose.yml`](docker-compose.yml) for local development.

Local development example:

```powershell
Copy-Item .env.example .env
docker compose up --build
```

With the sample env file, Central reads `AUTH_TOKEN_FILE=/run/relay-secrets/.auth_token`. If that file does not exist yet, Central generates a random token once and stores it in the shared hidden repo directory at [`.relay-secrets/.auth_token`](/d:/projects/relay_central/.relay-secrets/.auth_token).

Open the UI at `http://localhost:8000/`.

## Auth Token Behavior

Token precedence is:

1. `AUTH_TOKEN`
2. `AUTH_TOKEN_FILE`
3. fallback `change-me`

That means `AUTH_TOKEN` still works when you want to inject the secret directly, but `AUTH_TOKEN_FILE` is the safer default for local development and mounted-secret deployments.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `AUTH_TOKEN` | unset in `.env.example` | Direct bearer token override; if set, it takes precedence |
| `AUTH_TOKEN_FILE` | `/run/relay-secrets/.auth_token` | File containing the shared bearer token; created if missing |
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

## Local Compose Notes

The provided [`docker-compose.yml`](docker-compose.yml) mounts:

- `./data/backups` -> `/backups`
- `./data/staging` -> `/staging`
- `../.relay-secrets` -> `/run/relay-secrets`

That keeps uploaded archives on the host during local development and shares the auth token file with Edge.

## Running Without Docker

The app does not load `.env` automatically when run directly. Export the environment variables in your shell first, then start it from this directory:

```powershell
python -m app.main
```
