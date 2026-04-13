# RelayCentralizer Edge

Edge is the scanning and upload agent. It discovers backup jobs under a scan root, fingerprints directory contents, creates `tar.zst` archives, and uploads changed jobs to Central.

It also serves a small UI for creating, editing, and deleting `.upload_dir` marker files.

## What Edge Is Responsible For

- breadth-first scanning `SCAN_ROOT` for `.upload_dir` markers
- building job definitions from those marker files
- skipping unchanged jobs by comparing fingerprints
- keeping failed uploads in the spool for retry when configured
- resuming interrupted uploads by continuing from the last acknowledged byte offset
- optionally stopping and starting Docker Compose-managed directories around archive creation
- optionally pulling updated images before bringing a Compose stack back up
- uploading successful archives to Central

## Starting Edge

You can run Edge directly with Python, with a packaged executable, or with an installer package from a GitHub release.

For local Python development:

```powershell
python -m app.main
```

Edge now boots with built-in defaults and stores its local configuration in a local `settings.json` file managed through the web UI.

Open the UI at `http://localhost:8080/`.

If you do not want to manually activate and deactivate a virtual environment every time, use the included dev scripts instead. They create `.venv`, install requirements when needed, and can run Edge in the foreground or background without shell activation.

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

Command meanings:

- `setup` creates `.venv` and installs or updates Python requirements
- `run` starts Edge in the foreground
- `start` starts Edge in the background and writes logs to `.dev-runtime/edge.log`
- `stop` stops the background Edge process
- `restart` restarts the background Edge process
- `status` shows whether the background Edge process is running
- `clean` removes `.venv` and `.dev-runtime` when Edge is stopped

GitHub Actions builds Linux, macOS, and Windows artifacts through [`.github/workflows/edge-executable.yml`](../.github/workflows/edge-executable.yml), including installable `.deb`, `.pkg`, and Windows installer outputs.

## Running Release Builds

Each release publishes both installer packages and raw bundles. Installers are the recommended path for normal users.

Before starting Edge on any platform:

1. Start Edge and open `http://localhost:8080/`.
2. Put the same shared token used by Central into the Edge Settings section.
3. Set `CENTRAL_URL` to the reachable Central address.
4. Set `SCAN_ROOT` to the directory tree Edge should scan.

Linux `.deb`:

```bash
sudo dpkg -i relaycentralizer-edge-linux.deb
sudo systemctl enable --now relaycentralizer-edge
```

Linux raw bundle `.tar.gz`:

```bash
tar -xzf relaycentralizer-edge-linux.tar.gz
cd relaycentralizer-edge
./relaycentralizer-edge
```

macOS `.pkg`:

```bash
sudo installer -pkg relaycentralizer-edge-macos.pkg -target /
sudo launchctl bootstrap system /Library/LaunchDaemons/com.relaycentralizer.edge.plist
sudo launchctl kickstart -k system/com.relaycentralizer.edge
```

macOS raw bundle `.tar.gz`:

```bash
tar -xzf relaycentralizer-edge-macos.tar.gz
cd relaycentralizer-edge
./relaycentralizer-edge
```

Windows installer `.exe`:

1. Run `relaycentralizer-edge-windows-installer.exe`.
2. Optionally keep the startup-task option enabled.
3. Open `http://localhost:8080/` and save the Edge settings there.
4. If needed, register startup manually:

```powershell
powershell -ExecutionPolicy Bypass -File "C:\Program Files\RelayCentralizer Edge\Install-RelayCentralizerEdgeTask.ps1"
```

Windows raw bundle `.zip`:

```powershell
Expand-Archive .\relaycentralizer-edge-windows.zip -DestinationPath .
cd .\relaycentralizer-edge
.\relaycentralizer-edge.exe
```

After startup, open `http://localhost:8080/` and use the UI while Edge scans from `SCAN_ROOT`.

## Choosing `CENTRAL_URL`

Set `CENTRAL_URL` to whatever address the Edge runtime can actually reach:

- If Edge runs directly on the same host as a Central container that publishes port `8000`, use `http://127.0.0.1:8000`.
- If Edge and Central run in the same Compose project or shared Docker network, use `http://central:8000`.
- If Edge runs on a different machine, use Central's hostname or IP instead.

## Auth Token Behavior

Edge stores its auth token in its own local `settings.json` file.

- The token is set in the Edge Settings section of the local web UI.
- The token value must match the token file configured on Central.
- Edge does not read secrets from Central's filesystem.

## How Job Discovery Works

- Edge performs a breadth-first scan starting at `SCAN_ROOT`.
- Every directory containing a `.upload_dir` file becomes a backup job root.
- Once a parent directory is selected as a job, nested `.upload_dir` files under that parent are ignored.
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

## Host Docker CLI Requirement

When `is_docker_composed: true` is enabled for a job, Edge expects the host `docker` CLI with Compose support to be available on that machine. Edge invokes `docker compose ...` directly from the host runtime and does not run its own Docker daemon.

## Scheduler Behavior

- `CRON_SCHEDULE` uses a 5-field cron expression.
- Edge runs one backup cycle immediately on startup.
- Scheduled runs are serialized so overlapping cycles do not run at the same time.
- The schedule may not be more frequent than every 5 minutes.
- The UI's `Run Backup Cycle Now` action goes through the same scheduler controls and also clears any manual-intervention blocks for one explicit retry attempt.

## Settings Defaults

Edge starts with built-in defaults and saves any changes you make from the web UI into its local `settings.json` file.

| Setting | Default | Purpose |
| --- | --- | --- |
| `EDGE_ID` | `edge-01` | Namespace sent to Central |
| `SCAN_ROOT` | `/home`, `/Users`, or `C:\Users` | Root directory Edge scans for `.upload_dir` files, depending on platform |
| `CENTRAL_URL` | `http://127.0.0.1:8000` | Base URL for Central |
| `AUTH_TOKEN` | empty | Shared bearer token configured in the Edge settings UI |
| `CRON_SCHEDULE` | `0 2 * * *` | Backup schedule inside the Edge runtime |
| `STATE_DIR` | Platform state dir | Persistent job state and retry metadata |
| `SPOOL_DIR` | Platform cache dir + `spool` | Temporary archive storage before successful upload |
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
| `HTTP_HOST` | `127.0.0.1` | Bind address |
| `HTTP_PORT` | `8080` | Listen port |

## HTTP Surface

- `GET /` - HTML job-management UI
- `GET /health` - health check
- `GET /api/directories` - scan-root view, job configs, job state, and scheduler status
- `POST /api/settings` - save local Edge settings from the UI
- `POST /api/jobs` - create or update a `.upload_dir`
- `DELETE /api/jobs?relative_path=...` - remove a `.upload_dir`
- `POST /api/run-now` - request an immediate backup cycle

## Installer Notes

Release builds include:

- Linux `.deb` packages with a `systemd` service unit at `relaycentralizer-edge.service`
- macOS `.pkg` installers with a `launchd` plist at `com.relaycentralizer.edge.plist`
- Windows installer executables that can register a startup scheduled task

The installer packages start Edge with built-in defaults so the local UI is available immediately. Open that UI, save the settings you want, and then leave the installed service running.

## Running Compose Support On The Edge Host

If you enable `is_docker_composed: true`, the Edge runtime must be able to reach the Docker CLI and the selected directory's Compose file.

That usually means installing Edge on the same host that owns the Compose project and making sure `docker compose` works for the user or service context running Edge.
