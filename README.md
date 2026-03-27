# RelayCentralizer

RelayCentralizer is a lightweight distributed backup proof of concept with two services:

- `Edge` runs near the source data, discovers backup jobs from the filesystem, fingerprints them, creates `tar.zst` snapshots, and uploads changed jobs.
- `Central` receives snapshots over HTTP, stages them safely, stores them on local disk, and prunes older snapshots per job.

The POC is intentionally simple, file-based, and easy to inspect. There is no UI, database, restore API, or cloud storage integration yet.

## Architecture

### Edge

Edge scans a configured root directory such as `/home` for directories containing `.upload_dir`. Once a directory contains `.upload_dir`, that directory becomes a backup `job` and recursion stops there.

For each discovered job, Edge:

1. Loads optional YAML settings from `.upload_dir`
2. Builds a filtered list of regular files
3. Computes a deterministic manifest fingerprint from relative path, size, and mtime
4. Skips unchanged jobs
5. Creates a `tar.zst` snapshot for changed jobs
6. Uploads the snapshot to Central
7. Persists minimal per-job state in JSON
8. Retries a pending archive from the spool directory on later runs if a prior upload failed

### Central

Central exposes:

- `GET /health`
- `POST /backup/upload`

On upload, Central:

1. Verifies a shared bearer token
2. Validates and sanitizes `edge_id` and `job_name`
3. Streams the archive to a staging file first
4. Atomically moves the finished file into permanent storage
5. Stores snapshots under `<BACKUP_ROOT>/<edge_id>/<job_name>/`
6. Applies per-job retention after a successful commit

The storage layer is structured behind a backend interface. This POC implements only a local filesystem backend.

## Discovery With `.upload_dir`

`Edge` enrollment is filesystem-driven. A directory becomes a job when it contains `.upload_dir`.

Example:

```text
/home/
  navidrome/
    .upload_dir
    config.toml
  jellyfin/
    .upload_dir
    data/
  random/
    note.txt
```

Discovered jobs:

- `/home/navidrome`
- `/home/jellyfin`

Skipped:

- `/home/random`

`.upload_dir` is both a marker file and optional YAML config. If the file is empty, defaults are used.

Example:

```yaml
job_name: navidrome
exclude:
  - "*.log"
  - "cache/"
  - "tmp/"
include_hidden: true
follow_symlinks: false
```

Defaults:

- `job_name`: directory basename
- `exclude`: empty list
- `include_hidden`: `true`
- `follow_symlinks`: `false`

`.upload_dir` itself is never included in the archive.

## Configuration

Service-specific example env files are included at:

- `central/.env.example`
- `edge/.env.example`

A combined reference file also exists at `.env.example`.

### Central

```env
AUTH_TOKEN=change-me
STORAGE_BACKEND=local
BACKUP_ROOT=/backups
RETENTION_KEEP_LAST=3
LOG_LEVEL=INFO
MAX_UPLOAD_SIZE_MB=2048
STAGING_DIR=/staging
```

### Edge

```env
EDGE_ID=edge-01
SCAN_ROOT=/scan
CENTRAL_URL=http://central:8000
AUTH_TOKEN=change-me
INTERVAL_SECONDS=3600
STATE_DIR=/data/state
SPOOL_DIR=/data/spool
LOG_LEVEL=INFO
MAX_DEPTH=10
KEEP_LOCAL_PENDING=true
```

## Running With Docker Compose

1. Copy the example env files:

```powershell
Copy-Item central/.env.example central/.env
Copy-Item edge/.env.example edge/.env
```

2. Create a sample job in the scan root:

```powershell
New-Item -ItemType Directory -Force .data/edge_scan_root/navidrome | Out-Null
Copy-Item examples/upload_dir.example.yml .data/edge_scan_root/navidrome/.upload_dir
Set-Content .data/edge_scan_root/navidrome/config.toml "demo=true"
```

3. Start the stack:

```powershell
docker compose up --build
```

Central will listen on `http://localhost:8000`.

Snapshots will be stored under:

```text
.data/central_backups/<edge_id>/<job_name>/
```

The compose file already includes Docker log rotation settings through the `json-file` driver.

## Example Workflow

1. Add `.upload_dir` inside a directory under the Edge scan root.
2. Edge discovers the directory as a job and stops descending there.
3. On the first run, Edge creates a snapshot and uploads it.
4. On later runs, unchanged jobs log `skipped_unchanged`.
5. When files change, Edge creates a new snapshot.
6. Once more than three snapshots exist for a job, Central prunes older ones after each successful commit.

## Project Layout

```text
.
+-- README.md
+-- docker-compose.yml
+-- .env.example
+-- central/
¦   +-- Dockerfile
¦   +-- requirements.txt
¦   +-- .env.example
¦   +-- app/
+-- edge/
¦   +-- Dockerfile
¦   +-- requirements.txt
¦   +-- .env.example
¦   +-- app/
+-- examples/
    +-- upload_dir.example.yml
```

## Running Services Manually

Central:

```powershell
cd central
python -m app.main
```

Edge one-shot:

```powershell
cd edge
python -m app.main --once
```

Edge dry run:

```powershell
cd edge
python -m app.main --once --dry-run
```

## Known Limitations

- Bearer token auth is shared and static for the whole POC.
- Only the local filesystem storage backend is implemented.
- There is no restore API yet.
- Edge state is a single JSON file, which is simple but not optimized for large fleets.
- Fingerprints are manifest-based and trigger full snapshot uploads, not delta transfers.
- Symlink following is supported for traversal, but this POC does not do advanced symlink policy controls beyond basic include/skip behavior.
- No formal automated test suite is included yet; this repo currently relies on lightweight runtime verification.
