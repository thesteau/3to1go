# RelayCentralizer

RelayCentralizer is a backup workflow with a containerized receiver and a host-native edge agent:

- `central/` is the receiving service. It accepts snapshot uploads from Edge, stages them safely, stores them by `edge_id/job_name`, and applies retention.
- `edge/` is the device-side agent. It runs directly on the source host, breadth-first scans a configured root for `.upload_dir` markers, builds `tar.zst` archives for changed jobs, and uploads them to Central.

The two components are meant to run separately:

- Central runs wherever you want backups collected and retained.
- Edge runs on each device or host that should produce backups.

## End-To-End Flow

1. An operator creates a `.upload_dir` file in a directory under the Edge scan root.
2. Edge breadth-first scans from that root and discovers every matching job directory.
3. Edge builds a fingerprint of the selected files based on path and size. If nothing changed since the last successful upload, the job is skipped entirely.
4. If `is_docker_composed: true` and that same directory contains `docker-compose.yml` or `compose.yml`, Edge stops the stack before archiving it, optionally pulls updates, and brings it back up afterward.
5. Edge creates a `tar.zst` archive and encrypts it with AES-256-GCM before writing it to the upload spool.
6. Edge uploads the encrypted archive to Central over HTTP.
7. Central stages the upload, verifies the checksum, commits it into the backup store, and prunes older snapshots for that job.
8. Operators browse, download, and delete snapshots from the Central web UI. Encrypted archives are decrypted in-browser using the Edge encryption key.

## Storage Philosophy

Central is intentionally focused on durable local storage and retention. If you want copies in S3, Google Drive, Dropbox, or another remote system, the recommended approach is to sync or replicate Central `BACKUP_ROOT` with a separate service, scheduled job, or host-level script.

That keeps RelayCentralizer focused on backup intake and retention instead of turning Central into a multi-provider sync engine.

## Auth Token

RelayCentralizer uses a shared token between Central and Edge.

- Central reads `AUTH_TOKEN_FILE` from its own host or container filesystem.
- If Central's token file does not exist, Central creates it once and reuses it on later startups.
- Each Edge device stores the same token in its local Edge settings UI.
- The token value must match between Central and each Edge device.

Edge never reads auth data from Central's filesystem. Central only bootstraps its own local token file and does not write token files for Edge devices.

## `.upload_dir` At A Glance

A `.upload_dir` file is the marker that tells Edge: back up this directory.

By default, Edge scans from a platform-appropriate home-users root such as `/home`, `/Users`, or `C:\Users` unless you change it in the Edge settings UI.

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

You can also use an empty `.upload_dir` file. In that case, Edge uses the directory name as `job_name`, `include_hidden: true`, `follow_symlinks: false`, `is_docker_composed: false`, and `update_container_on_packup: false`.

## How To Use `.upload_dir`

1. Make sure the directory you want to back up is visible under Edge `SCAN_ROOT`.
2. Create a file named `.upload_dir` inside that directory.
3. Add the YAML fields you want, or leave it empty for the default behavior.
4. If the directory is a Docker Compose project, set `is_docker_composed: true` only when that same directory contains `docker-compose.yml` or `compose.yml`.
5. Set `update_container_on_packup: true` only if you also want Edge to run `docker compose pull` before it brings the stack back up.
6. Put the same token value into Central's `AUTH_TOKEN_FILE` and into the Edge settings UI.
7. Start Edge.
8. Check the Edge UI to confirm the job was discovered.
9. Check the Central UI to confirm the archive was uploaded and stored.

Example path:

```text
/scan/photos/.upload_dir
```

That tells Edge to treat `/scan/photos` as a backup job root.

## Encryption

Every Edge generates an AES-256-GCM key on first run and stores it in its config directory (`encryption.key` alongside `settings.json`). The key is displayed in the Edge web UI with a one-click copy button.

Archives are encrypted before upload. Central stores and serves the encrypted blobs without ever seeing plaintext. When downloading a snapshot from the Central UI, the browser detects the encrypted format automatically and prompts for the key. Decryption happens entirely in-browser — the key never leaves the browser or travels over the network.

If the key file is lost, previously uploaded archives cannot be recovered. Back up `encryption.key` alongside other critical configuration.

## Repo Layout

- [`central/`](central/) - receiver API, storage, retention, web UI for browsing and downloading snapshots
- [`edge/`](edge/) - scan agent, scheduler, upload pipeline, encryption, Compose-aware backup hooks, and Edge job-management UI
- [`deploy-example/central/`](deploy-example/central/) - production Compose example for Central
- [`deploy-example/edge/`](deploy-example/edge/) - production Compose example for Edge

## Running The Services

Central runs as a Docker container. Edge can run as a host-native packaged executable, installer, or Docker container.

GitHub Actions builds:
- [`edge-executable.yml`](.github/workflows/edge-executable.yml) — Linux `.deb`, macOS `.pkg`, and Windows `.exe` installer packages published on release tags
- [`docker-image.yml`](.github/workflows/docker-image.yml) — Central Docker image pushed to GHCR on every push to `main`
- [`edge-docker-image.yml`](.github/workflows/edge-docker-image.yml) — Edge Docker image pushed to GHCR on every push to `main`

Edge starts with built-in defaults and exposes its local web UI on `http://localhost:8080/`. Settings are saved there.

If running Edge and Central as Docker containers on the same host, use the deploy examples in [`deploy-example/`](deploy-example/). Central binds to port `6555` and Edge to port `6556` in those examples to avoid conflicts.

## Running Edge Releases

Each Edge release publishes two asset types per platform:

- installer package: best for normal users
- raw bundle: best for manual testing or custom service setup

Before starting Edge on any platform:

1. Create the same auth token value used by Central.
2. Start Edge and open the local UI at `http://localhost:8080/`.
3. Enter that token into the Edge Settings section.
4. Set `CENTRAL_URL` so Edge can reach Central.
5. Set `SCAN_ROOT` to the home directory, root directory, or other path you want Edge to scan.

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
2. Leave the startup-task option enabled if you want Edge to start automatically.
3. Open `http://localhost:8080/` and save the Edge settings there.
4. If you did not enable auto-start in the installer, run:

```powershell
powershell -ExecutionPolicy Bypass -File "C:\Program Files\RelayCentralizer Edge\Install-RelayCentralizerEdgeTask.ps1"
```

Windows raw bundle `.zip`:

```powershell
Expand-Archive .\relaycentralizer-edge-windows.zip -DestinationPath .
cd .\relaycentralizer-edge
.\relaycentralizer-edge.exe
```

After Edge starts, open `http://localhost:8080/`, create `.upload_dir` markers in the directories you want backed up, and confirm uploads arrive in Central.

For local development only:

1. Start Central from [`central/`](central/).
2. Create the token value for Central in `central/secrets/relay_auth_token`.
3. Enter the same token value in the Edge settings UI.
4. Start Edge from [`edge/`](edge/) or install a packaged Edge release on the host.
5. If you run Central in a container, mount Central's token file into that container read-only.
6. Point Edge `CENTRAL_URL` at the Central service it can reach.

The bundled Central compose file is a convenience wrapper for local setup, not the core backup workflow.

## Where To Start

- Central setup and API details: [`central/README.md`](central/README.md)
- Edge setup, job format, scheduler behavior, and Compose-aware backup flow: [`edge/README.md`](edge/README.md)
