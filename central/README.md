# RelayCentralizer Central

Central is the machine that receives and keeps backups.

Think of it as the storage side of the system:

- Edge makes encrypted backup archives.
- Central accepts those uploads.
- Central stores them on disk.
- Central gives you a web UI to browse, download, and delete snapshots.

## What Central Does

Central is responsible for:

- checking signed Edge credentials on incoming uploads
- staging uploads safely before commit
- verifying archive checksums
- storing snapshots under `<BACKUP_ROOT>/<edge_id>/<edge_instance_id>/<job_name>/`
- pruning old snapshots according to retention settings
- showing the stored snapshots in a simple web UI

Central stores what Edge sends. If Edge encryption is enabled, Central stores encrypted blobs and never sees plaintext.

Central keeps its internal metadata in PostgreSQL on the Central machine. That database covers the snapshot index, Edge registration registry, users, and saved runtime settings. Edge does not connect to that database directly.

Central also keeps its own advanced runtime settings in its database. In the bundled Docker layout the database lives under persistent volumes, so values changed in the UI persist across `docker compose down` and `docker compose up`.

## First-Time Setup

The easiest way to start is with Docker Compose.

### 1. Prepare persistent storage

Central generates its Ed25519 issuer key on startup if it does not exist yet. After starting, use Central's UI (`Mint Edge Credential`) to generate a signed JWT for each Edge.

The bundled Central Compose file mounts `./secrets` as a directory at `/run/secrets` so first-boot key generation works automatically.

### 2. Start the service

For normal deployment with the published image, use [`deploy-example/central/`](../deploy-example/central/):

```powershell
Copy-Item .env.example .env
docker compose up -d
```

Open the UI at `http://localhost:6555/`.

That deploy example already includes:

- a PostgreSQL sidecar for Central metadata, users, and saved runtime settings
- mounted storage for backups, staging, metadata, and secrets
- the user-facing `.env.example` with only the essentials

If you are contributing and want to build from this repo instead, use the bundled files in [`central/`](./):

```powershell
Copy-Item .env.example .env
docker compose up --build
```

Open the UI at `http://localhost:6555/`.

The bundled [`docker-compose.yml`](docker-compose.yml) is the contributor/developer starting point, not the main user deployment path.

## If Edge Runs In Docker On The Same Host

If Edge is in a separate Docker Desktop project on the same machine, it will often reach Central at:

```text
http://host.docker.internal:6555
```

## Trusting Internal HTTPS Certificates

Central can trust private/internal HTTPS services, such as a self-hosted ntfy endpoint, without baking certificates into the image.

Admins can upload trusted CA certificates from the Central UI:

1. Open **Trusted Certificates**.
2. Upload a `.crt` PEM CA/root certificate.
3. Test the HTTPS integration again, for example from **Configure ntfy**.

Uploaded certificates are stored under:

```text
/config/trusted-certs
```

The container installs them into the Debian trust store immediately after upload. On later container starts, the entrypoint installs those saved certificates again before Central starts.

You can also place one or more internal CA certificates in the mounted `certs` directory for automated deployments:

```text
central/certs/home-ca.crt
```

For the published deployment example, use:

```text
deploy-example/central/certs/home-ca.crt
```

Only files ending in `.crt` are installed. Use the CA/root certificate that issued the service certificate, not usually the service certificate itself.

Then configure ntfy with the trusted HTTPS URL, for example:

```text
https://ntfy.home
```

After adding a certificate through the mounted `certs` directory, restart Central:

```bash
docker compose up -d --force-recreate central
```

You can test trust from inside the container:

```bash
docker compose exec central curl -v https://ntfy.home
docker compose exec central curl -v -d "Central ntfy test" https://ntfy.home/relay-centralizer-uploads
```

Advanced overrides:

| Variable | Default |
| --- | --- |
| `RELAY_EXTRA_CA_DIR` | `/run/relay-certs` |
| `RELAY_EXTRA_CA_FILE` | unset |

## What The Central UI Is For

The UI is for operators, not for configuring Edge.

You use it to:

- see which Edges and jobs have uploaded snapshots
- download a stored snapshot
- paste and verify an Edge decryption key in the browser
- delete individual snapshots

If Central knows an Edge's key fingerprint, the browser can warn you before decryption if you pasted the wrong key for that Edge.

## Web UI Accounts

The Central web UI starts with the default account `admin` / `admin`. On first sign-in, change it immediately. The password change form requires the current password, a new password, and confirmation of the new password. Passwords must be at least 5 characters and cannot be only spaces. Any account using `admin` as its password is always forced to change it after sign-in.

Admins can add users, remove users, reset other users' passwords, and assign admin access from **Users & Access**. When an admin resets another user's password, that user is signed out and must change the temporary password on next sign-in.

If every admin password is lost, reset a password from the server/container:

```bash
docker compose exec central python -m app.scripts.reset_password admin
```

The command prompts for the new password twice, clears active sessions for that user, and requires the user to change the password after signing in. For automation, provide the password through an environment variable:

```bash
docker compose exec -e RESET_PASSWORD='new-temporary-password' central python -m app.scripts.reset_password admin --password-env RESET_PASSWORD
```

## Important Limits And Design Choices

### Edge credentials

Central signs per-Edge credentials with its issuer key.

- Mint Edge credentials from Central's UI and paste them into the Edge settings UI.
- The credential is displayed once when minted. Central stores only database metadata needed to validate and revoke it.
- Revoking the credential next to one instance also revokes that token for any other instances that reused it.
- Expired credential rows are reaped from the database about every 12 hours.

### Local storage only

Central writes to the local filesystem rooted at `BACKUP_ROOT`.

If you want off-site or cloud copies, the intended pattern is to sync `BACKUP_ROOT` with a separate tool after Central writes it.

### Metadata backend

Final snapshot archives still live on disk under `BACKUP_ROOT`.

Central requires PostgreSQL on the Central host for internal metadata, users, and saved runtime settings.

Existing snapshot files remain on disk under `BACKUP_ROOT`; Central stores the live snapshot index and Edge registry in PostgreSQL.

### Settings precedence

Central uses two different configuration sources on purpose:

- Infrastructure settings come from the environment and Docker layout: PostgreSQL credentials, backup root, staging dir, and the HTTP bind.
- Operator settings come from Central's database: retention, logging level, upload limits, upload session TTL, and cleanup interval.

That means `docker compose down` and `docker compose up` do not reset the UI-edited Central settings, because those values are not being pulled from the env file anymore.

### Edge identity protection

Newer Edge builds send a stable `edge_instance_id`.

Central uses that to keep each real Edge installation separate under a shared `edge_id` when needed. Multiple machines can intentionally reuse the same `edge_id`, but Central now isolates their snapshots and registrations per `edge_instance_id`.

## Storage Layout

Snapshots are stored like this:

```text
<BACKUP_ROOT>/<edge_id>/<edge_instance_id>/<job_name>/<job_name>__<timestamp>__<fingerprint>.tar.zst
```

Example:

```text
/backups/edge-01/photos/photos__2026-03-29T09-00-00Z__1a2b3c4d.tar.zst
```

Uploads are written to `STAGING_DIR` first and moved into final storage only after verification succeeds.

## Main Settings

These are the environment variables most people care about first:

| Variable | Default | What it means |
| --- | --- | --- |
| `POSTGRES_USER` | `relay` | PostgreSQL username for Central metadata, users, and settings |
| `POSTGRES_PASSWORD` | `change-this-password` | PostgreSQL password for Central metadata, users, and settings |

These Central values are edited in the Central UI and saved in Central's database instead of `.env`:

| Setting in Central UI | Default |
| --- | --- |
| Retention Keep Last | `3` |
| Log Level | `INFO` |
| Max Upload Size MB | `2048` |
| Upload Chunk Size MB | `8` |
| Upload Session TTL Hours | `24` |
| Upload Cleanup Interval Seconds | `300` |

Advanced environment and layout details:

| Variable | Default |
| --- | --- |
| `INDEX_DATABASE_URL` | unset |
| `INDEX_DATABASE_HOST` | `postgres` |
| `INDEX_DATABASE_PORT` | `5432` |
| `INDEX_DATABASE_NAME` | `relaycentral` |
| `POSTGRES_DB` | `relaycentral` |
| `BACKUP_ROOT` | `/backups` |
| `STAGING_DIR` | `/staging` |
| `HTTP_HOST` | `0.0.0.0` in Docker, `127.0.0.1` otherwise |
| `HTTP_PORT` | `6555` |

## API Surface

Useful endpoints:

- `GET /` - Central web UI
- `GET /api/overview` - JSON summary of stored snapshots
- `GET /health/ready` - lightweight readiness check
- `GET /health` - health check
- `GET /api/snapshots/{edge_id}/{edge_instance_id}/{job_name}/{filename}` - download an instance-scoped snapshot
- `DELETE /api/snapshots/{edge_id}/{edge_instance_id}/{job_name}/{filename}` - delete an instance-scoped snapshot
- `POST /backup/uploads/initiate` - start or resume an upload
- `PUT /backup/uploads/{upload_id}/chunk?offset=...` - append upload bytes
- `POST /backup/uploads/{upload_id}/finalize` - finalize and commit the upload

Newer Edge clients also send:

- `edge_instance_id`
- `encryption_key_fingerprint`

Central uses those fields to keep registrations and snapshots separated per instance and improve decryption-key verification in the UI.

## Compose Mounts

The bundled Compose example mounts:

- `./data/backups` to `/backups`
- `./data/staging` to `/staging`
- `./data/postgres` for PostgreSQL metadata, users, and settings
- `./secrets` to `/run/secrets`
- `./certs` to `/run/relay-certs` for optional internal CA certificates

After the first start, Central writes the generated issuer key to `./secrets/relay_issuer.key`.
