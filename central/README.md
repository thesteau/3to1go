# RelayCentralizer Central

Central is the receiving service. It accepts backup uploads from Edge, stages them to disk, atomically commits them into local storage, and prunes older snapshots per job namespace.

## What Central Is Responsible For

- authenticating uploads from Edge with the configured bearer token
- staging incoming archives before commit
- keeping resumable upload-session metadata in `STAGING_DIR/uploads/`
- rejecting already-committed duplicate archives within the same `edge_id/job_name` namespace by checksum
- storing snapshots under `<BACKUP_ROOT>/<edge_id>/<job_name>/`
- pruning older snapshots according to `RETENTION_KEEP_LAST`
- exposing a web UI to browse, download, and delete stored snapshots per edge and job

Central stores whatever Edge sends. If Edge has encryption enabled (the default), Central holds encrypted blobs and never sees plaintext. Decryption happens in the browser at download time using the key from the Edge UI.

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

`AUTH_TOKEN_FILE` points to Central's local token file path. For the bundled Docker Compose setup, create that file on the host first so the container can mount it read-only.

Then start Central:

```powershell
docker compose up --build
```

Open the UI at `http://localhost:8000/`.

## Same-Host Edge Note

If you also run an Edge container on this same Docker Desktop host, but in a separate Compose project, that Edge container will often reach Central with:

```text
http://host.docker.internal:8000
```

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
| `AUTH_TOKEN_FILE` | `/run/secrets/relay_auth_token` | File containing the bearer token for upload authentication |
| `STORAGE_BACKEND` | `local` | Storage backend selector; only `local` is implemented |
| `BACKUP_ROOT` | `/backups` | Final snapshot storage location |
| `RETENTION_KEEP_LAST` | `3` | Number of snapshots to keep per `edge_id/job_name` |
| `LOG_LEVEL` | `INFO` | Application log level |
| `MAX_UPLOAD_SIZE_MB` | `2048` | Maximum accepted upload size |
| `UPLOAD_CHUNK_SIZE_MB` | `8` | Recommended chunk size returned to Edge for resumable uploads |
| `UPLOAD_SESSION_TTL_HOURS` | `24` | How long incomplete or completed upload-session metadata is retained before cleanup |
| `UPLOAD_CLEANUP_INTERVAL_SECONDS` | `300` | How often Central runs its background cleanup loop for expired upload sessions |
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

- `GET /` - web UI for browsing, downloading, and deleting snapshots by edge and job
- `GET /api/overview` - JSON summary of storage paths, retention, and stored snapshots with size and mtime per snapshot
- `GET /health/ready` - lightweight readiness check for container healthchecks
- `GET /health` - health check; returns `503` if the storage backend is unavailable
- `GET /api/snapshots/{edge_id}/{job_name}/{filename}` - stream a snapshot archive for download
- `DELETE /api/snapshots/{edge_id}/{job_name}/{filename}` - delete a specific snapshot (requires bearer token)
- `POST /backup/uploads/initiate` - create or resume an idempotent upload session
- `PUT /backup/uploads/{upload_id}/chunk?offset=...` - append one chunk at the declared byte offset
- `POST /backup/uploads/{upload_id}/finalize` - atomically commit a completed upload into final storage

### Downloading Encrypted Snapshots

The Central UI detects encrypted archives automatically by checking a magic header. When a download is initiated for an encrypted archive, the browser prompts for the Edge encryption key. Decryption happens entirely in-browser using the Web Crypto API — the key is never sent to Central.

The resumable upload flow expects:

- `Authorization: Bearer <token from AUTH_TOKEN_FILE>`
- an initiate payload containing `edge_id`, `job_name`, `fingerprint`, `timestamp`, `archive_format`, `archive_size_bytes`, `archive_sha256`, and `idempotency_key`
- chunk bodies sent as raw `application/octet-stream`
- finalize after the last acknowledged offset reaches the declared archive size; Central verifies the final archive checksum before commit

## Local Compose Notes

The provided [`docker-compose.yml`](docker-compose.yml) mounts:

- `./data/backups` -> `/backups`
- `./data/staging` -> `/staging`
- `./secrets/relay_auth_token` -> `/run/secrets/relay_auth_token` (read-only)

Create `./secrets/relay_auth_token` on the host before you start the Compose stack.

If an Edge container runs separately on this same Docker Desktop machine, point that Edge instance at `http://host.docker.internal:8000`.
