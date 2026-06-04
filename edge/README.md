# RelayCentralizer Edge

Edge is the machine-side backup agent.

It runs on the computer that has the files you want to protect, watches for folders marked with `.upload_dir`, creates encrypted archives, and uploads them to Central.

It also includes a small local web UI for setup and job management.

## What Edge Does

Edge is responsible for:

- scanning a root directory for `.upload_dir` files
- turning those markers into backup jobs
- skipping jobs that have not changed
- creating `tar.zst` archives when something changed
- encrypting those archives before upload
- retrying interrupted uploads

## First-Time Setup

If you are seeing this for the first time, use this order:

1. Start Central somewhere reachable.
2. Start Edge on the machine you want to back up.
3. Open the Edge UI at `http://localhost:6556/`.
4. Set `CENTRAL_URL`.
5. Enter the same auth token Central uses.
6. Pick a unique `EDGE_ID`.
7. Create `.upload_dir` files in folders you want backed up.
8. Run a backup cycle and confirm the snapshots appear on Central.

## The Most Important Thing To Understand

Edge does not use one big backup manifest.

Instead, it scans a directory tree and treats any folder containing `.upload_dir` as a job. That means the normal workflow is:

- choose a root to scan
- drop `.upload_dir` into folders you care about
- let Edge discover them automatically

## What A `.upload_dir` File Does

A `.upload_dir` file marks one folder as a backup job.

Example:

```text
/scan/photos/.upload_dir
```

Minimal example:

```yaml
job_name: photos
```

An empty `.upload_dir` is valid. If it is empty, Edge uses the folder name as the job name.

Common example:

```yaml
job_name: photos
exclude:
  - "*.tmp"
  - cache/**
include_hidden: true
follow_symlinks: false
```

## Job Discovery Rules

Edge scans from `SCAN_ROOT`.

- Every directory containing `.upload_dir` becomes a job.
- If a parent directory is already a job, nested `.upload_dir` files under it are ignored.
- If nothing changed since the last successful upload, Edge skips that job.

That keeps nested jobs from fighting each other and avoids uploading the same unchanged data over and over.

## Encryption

Each Edge creates an `encryption.key` file on first run.

- Archives are encrypted before upload.
- Central stores encrypted blobs.
- The Edge UI shows the key and its fingerprint.
- The Central UI can verify the fingerprint before decrypting a download.

Back up the key file. If you lose it, old encrypted snapshots from that Edge cannot be decrypted.

Key locations by install type:

| Method | Location |
| --- | --- |
| Docker | `/config/encryption.key` |
| Linux native | `~/.config/RelayCentralizerEdge/encryption.key` |
| macOS native | `~/Library/Application Support/RelayCentralizerEdge/encryption.key` |
| Windows native | `%APPDATA%\RelayCentralizerEdge\encryption.key` |

## Auth And Identity

### Web UI accounts

The Edge web UI starts with the default account `admin` / `admin`. On first sign-in, change it immediately. The password change form requires the current password, a new password, and confirmation of the new password. Passwords must be at least 5 characters and cannot be only spaces. Any account using `admin` as its password is always forced to change it after sign-in.

Admins can add users, remove users, reset other users' passwords, and assign admin access from **Users & Access**. When an admin resets another user's password, that user is signed out and must change the temporary password on next sign-in.

If every admin password is lost, reset a password from the server/container:

```bash
docker compose exec edge python -m app.scripts.reset_password admin
```

The command prompts for the new password twice, clears active sessions for that user, and requires the user to change the password after signing in. For automation, provide the password through an environment variable:

```bash
docker compose exec -e RESET_PASSWORD='new-temporary-password' edge python -m app.scripts.reset_password admin --password-env RESET_PASSWORD
```

### Shared token

Edge can get the shared auth token in two ways:

- entered in the Edge UI and stored in the local Edge database
- preloaded from `AUTH_TOKEN_FILE`, which is how the bundled Docker examples work

- The token must match Central's token.
- Edge does not read secrets from Central's filesystem.
- Token rotation is global across all Edges using that Central.

### Unique `EDGE_ID`

Each installation needs its own `EDGE_ID`.

This matters because Central groups by `edge_id`, but stores snapshots under each Edge installation's `edge_instance_id`. That means multiple machines can intentionally share one `EDGE_ID` for grouping while still keeping their snapshots separate.

## Choosing `CENTRAL_URL`

Use the address Edge can actually reach:

- same host as Central container with published port: `http://127.0.0.1:6555`
- same Docker network as Central: `http://central:6555`
- different machine: use Central's hostname or IP
- separate Docker Desktop projects on the same host: often `http://host.docker.internal:6555`

## Running Edge

### Local Python development

```powershell
python -m app.main
```

Edge starts with built-in defaults and exposes its UI on `http://localhost:6556/`.

### Dev helper scripts

If you do not want to manage a virtualenv manually, use the provided helper scripts.

Windows PowerShell:

```powershell
.\dev-edge.ps1 setup
.\dev-edge.ps1 run
.\dev-edge.ps1 start
.\dev-edge.ps1 stop
.\dev-edge.ps1 status
```

Windows Command Prompt:

```cmd
dev-edge.cmd setup
dev-edge.cmd run
dev-edge.cmd start
dev-edge.cmd stop
dev-edge.cmd status
```

macOS and Linux:

```bash
chmod +x ./dev-edge.sh
./dev-edge.sh setup
./dev-edge.sh run
./dev-edge.sh start
./dev-edge.sh stop
./dev-edge.sh status
```

### Docker

For normal deployment with the published image, use [`deploy-example/edge/`](../deploy-example/edge/):

```bash
docker compose up -d
```

Open the UI at `http://localhost:6556/`.

In that example, Edge binds to port `6556` so it does not conflict with Central on `6555`.

If you are contributing and want to build Edge from this repo directly, use the bundled files in [`edge/`](./):

```powershell
Copy-Item .env.example .env
docker compose up --build
```

Open the UI at `http://localhost:6556/`.

Place the shared token file at `./secrets/relay_auth_token` before starting the stack.
In the bundled Docker examples, `AUTH_TOKEN_FILE` can be just the filename, such as `relay_auth_token`.
If you prefer a different filename, update `AUTH_TOKEN_FILE` in `.env` to match it.

## Release Builds

Edge release assets come in two styles:

- installer packages for normal use
- raw bundles for manual setup or testing

After installing or unpacking Edge:

1. Open `http://localhost:6556/`.
2. Set `CENTRAL_URL`.
3. Enter the shared auth token.
4. Set a unique `EDGE_ID`.
5. In the Docker examples, Edge automatically starts with `SCAN_ROOT=/scan`. You can change that later in the Edge UI if needed.

Windows installer startup can be registered manually with:

```powershell
powershell -ExecutionPolicy Bypass -File "C:\Program Files\RelayCentralizer Edge\Install-RelayCentralizerEdgeTask.ps1"
```

## Scheduler Behavior

- Edge runs once on startup.
- Scheduled runs use a 5-field cron expression.
- Runs are serialized so they do not overlap.
- The minimum schedule gap is 5 minutes.
- The UI's `Run Backup Cycle Now` button uses the same scheduler path and can clear manual retry blocks for one explicit retry attempt.

## Main Settings

Most people care about these first:

| Setting | Default | Meaning |
| --- | --- | --- |
| `EDGE_ID` | `edge-01` | Name sent to Central; should be unique per installation |
| `SCAN_ROOT` | `/scan` in Docker, platform-dependent otherwise | Root directory Edge scans for `.upload_dir` files |
| `CENTRAL_URL` | `http://127.0.0.1:6555` | Address of Central |
| `AUTH_TOKEN_FILE` | unset | Optional token file path, or just a filename under `/run/secrets` in the Docker examples |
| `AUTH_TOKEN` | empty | Shared bearer token when not using `AUTH_TOKEN_FILE` |
| Cron Schedule in the Edge UI | `0 2 * * 0` | Backup schedule, defaulting to Sunday at 2:00 AM |
| `HTTP_PORT` | `6556` | Local Edge UI port |

Additional settings:

| Setting | Default |
| --- | --- |
| `STATE_DIR` | platform state dir |
| `SPOOL_DIR` | platform cache dir + `spool` |
| `LOG_LEVEL` | `INFO` |
| `MAX_DEPTH` | `10` |
| `KEEP_LOCAL_PENDING` | `true` |
| `UPLOAD_CHUNK_SIZE_MB` | `8` |
| `MIN_UPLOAD_CHUNK_SIZE_MB` | `1` |
| `MAX_UPLOAD_CHUNK_SIZE_MB` | `16` |
| `UPLOAD_RETRY_MAX_ATTEMPTS` | `5` |
| `UPLOAD_RETRY_BASE_DELAY_SECONDS` | `5` |
| `UPLOAD_RETRY_MAX_DELAY_SECONDS` | `300` |
| `UPLOAD_CONNECT_TIMEOUT_SECONDS` | `10` |
| `UPLOAD_READ_TIMEOUT_PADDING_SECONDS` | `30` |
| `UPLOAD_MIN_THROUGHPUT_BYTES_PER_SECOND` | `262144` |
| `CIRCUIT_BREAKER_FAILURE_THRESHOLD` | `5` |
| `CIRCUIT_BREAKER_COOLDOWN_SECONDS` | `300` |
| `HTTP_HOST` | `0.0.0.0` in Docker, `127.0.0.1` otherwise |

## API Surface

- `GET /` - Edge web UI
- `GET /health` - health check
- `GET /api/directories` - discovered directories, job config, and job state
- `GET /api/encryption-key` - current encryption key and fingerprint
- `POST /api/settings` - save Edge settings
- `POST /api/jobs` - create or update a `.upload_dir`
- `DELETE /api/jobs?relative_path=...` - remove a `.upload_dir`
- `POST /api/run-now` - request an immediate backup cycle
