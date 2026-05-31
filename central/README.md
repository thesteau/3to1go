# RelayCentralizer Central

Central is the machine that receives and keeps backups.

Think of it as the storage side of the system:

- Edge makes encrypted backup archives.
- Central accepts those uploads.
- Central stores them on disk.
- Central gives you a web UI to browse, download, and delete snapshots.

## What Central Does

Central is responsible for:

- checking the shared auth token on incoming uploads
- staging uploads safely before commit
- verifying archive checksums
- storing snapshots under `<BACKUP_ROOT>/<edge_id>/<job_name>/`
- pruning old snapshots according to retention settings
- showing the stored snapshots in a simple web UI

Central stores what Edge sends. If Edge encryption is enabled, Central stores encrypted blobs and never sees plaintext.

Central can also keep its internal metadata in PostgreSQL on the Central machine. That metadata covers the snapshot index (`.relay_index`) and Edge registration registry (`.relay_registry`). Edge does not connect to that database directly.

Central also keeps its own advanced runtime settings in a local config file. In the bundled Docker layout that file lives under the mounted `./config` volume, so values changed in the UI persist across `docker compose down` and `docker compose up`.

## First-Time Setup

The easiest way to start is with Docker Compose.

### 1. Prepare the token file

Central expects `AUTH_TOKEN_FILE` to point to a file that contains the shared bearer token.

If the file does not exist yet, Central creates it on startup. That token must then be copied into each Edge that should talk to this Central.

The bundled Central Compose file mounts `./secrets` as a directory at `/run/secrets` so first-boot token generation works automatically.

### 2. Start the service

For local development:

```powershell
Copy-Item .env.example .env
docker compose up --build
```

Open the UI at `http://localhost:8000/`.

The bundled [`docker-compose.yml`](docker-compose.yml) is a local starting point, not a required production layout.

## If Edge Runs In Docker On The Same Host

If Edge is in a separate Docker Desktop project on the same machine, it will often reach Central at:

```text
http://host.docker.internal:8000
```

## What The Central UI Is For

The UI is for operators, not for configuring Edge.

You use it to:

- see which Edges and jobs have uploaded snapshots
- download a stored snapshot
- paste and verify an Edge decryption key in the browser
- delete individual snapshots

If Central knows an Edge's key fingerprint, the browser can warn you before decryption if you pasted the wrong key for that Edge.

## Important Limits And Design Choices

### Shared auth token

Central and all connected Edges share one bearer token.

- Every Edge must use the same token value.
- Rotating the token means updating every Edge.
- There is no per-edge revocation yet.

### Local storage only

Central writes to the local filesystem rooted at `BACKUP_ROOT`.

If you want off-site or cloud copies, the intended pattern is to sync `BACKUP_ROOT` with a separate tool after Central writes it.

### Metadata backend

Final snapshot archives still live on disk under `BACKUP_ROOT`.

Central's internal metadata can use either:

- file-backed metadata under `.relay_index` and `.relay_registry`
- PostgreSQL on the Central host for those same metadata records

When PostgreSQL credentials are configured, Central imports existing file-backed metadata into PostgreSQL on startup so the UI and duplicate detection keep working with existing snapshots.

### Settings precedence

Central uses two different configuration sources on purpose:

- Infrastructure settings come from the environment and Docker layout: auth token file, PostgreSQL credentials, backup root, staging dir, and the HTTP bind.
- Operator settings come from Central's saved config file: retention, logging level, upload limits, upload session TTL, and cleanup interval.

That means `docker compose down` and `docker compose up` do not reset the UI-edited Central settings, because those values are not being pulled from the env file anymore.

### Edge identity protection

Newer Edge builds send a stable `edge_instance_id`.

Central uses that to reserve each `edge_id` to one real Edge installation. If a second machine tries to reuse the same `edge_id`, Central rejects the upload instead of letting them silently share one namespace.

## Storage Layout

Snapshots are stored like this:

```text
<BACKUP_ROOT>/<edge_id>/<job_name>/<job_name>__<timestamp>__<fingerprint>.tar.zst
```

Example:

```text
/backups/edge-01/photos/photos__2026-03-29T09-00-00Z__1a2b3c4d.tar.zst
```

Uploads are written to `STAGING_DIR` first and moved into final storage only after verification succeeds.

## Main Settings

These are the ones most people care about first:

| Variable | Default | What it means |
| --- | --- | --- |
| `AUTH_TOKEN_FILE` | `/run/secrets/relay_auth_token` | File containing the shared bearer token |
| `BACKUP_ROOT` | `/backups` | Where final snapshots are stored |
| `RETENTION_KEEP_LAST` | `3` | How many snapshots to keep per `edge_id/job_name` |
| `STAGING_DIR` | `/staging` | Temporary upload staging area |
| `POSTGRES_USER` | `relay` | PostgreSQL username for Central metadata |
| `POSTGRES_PASSWORD` | `change-this-password` | PostgreSQL password for Central metadata |

Full settings table:

| Variable | Default |
| --- | --- |
| `INDEX_DATABASE_URL` | unset |
| `INDEX_DATABASE_HOST` | `postgres` |
| `INDEX_DATABASE_PORT` | `5432` |
| `INDEX_DATABASE_NAME` | `relaycentral` |
| `POSTGRES_DB` | `relaycentral` |

## API Surface

Useful endpoints:

- `GET /` - Central web UI
- `GET /api/overview` - JSON summary of stored snapshots
- `GET /health/ready` - lightweight readiness check
- `GET /health` - health check
- `GET /api/snapshots/{edge_id}/{job_name}/{filename}` - download a snapshot
- `DELETE /api/snapshots/{edge_id}/{job_name}/{filename}` - delete a snapshot
- `POST /backup/uploads/initiate` - start or resume an upload
- `PUT /backup/uploads/{upload_id}/chunk?offset=...` - append upload bytes
- `POST /backup/uploads/{upload_id}/finalize` - finalize and commit the upload

Newer Edge clients also send:

- `edge_instance_id`
- `encryption_key_fingerprint`

Central uses those fields to protect `edge_id` ownership and improve decryption-key verification in the UI.

## Compose Mounts

The bundled Compose example mounts:

- `./data/backups` to `/backups`
- `./data/staging` to `/staging`
- `./data/postgres` for PostgreSQL metadata storage
- `./secrets` to `/run/secrets`

After the first start, Central writes the generated token to `./secrets/relay_auth_token`.

If you prefer mounting a single token file instead of the whole directory, create that host file before starting the stack. Otherwise Docker may create a directory at that path and Central will refuse to start with a configuration error.
