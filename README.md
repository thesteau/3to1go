# RelayCentralizer

RelayCentralizer is a lightweight distributed backup proof of concept with two independent services:

- `Central`: receives uploaded backup snapshots, stores them on disk, and applies retention.
- `Edge`: discovers backup jobs from the filesystem, creates `tar.zst` snapshots, and uploads them to Central.

Each service is now packaged and documented separately so someone can work from `central/` or `edge/` directly without needing a repo-level Docker Compose file.

## Service Entry Points

- [central/README.md](/d:/projects/relay_central/central/README.md)
- [edge/README.md](/d:/projects/relay_central/edge/README.md)

## High-Level Behavior

### Edge

Edge scans a configured root directory for folders containing `.upload_dir`. Each such directory becomes a backup job. Edge fingerprints the job contents, skips unchanged jobs, creates a `tar.zst` archive for changed jobs, and uploads the snapshot to Central.

Edge now also serves a small HTML UI that lets users:

- see the current scan root pathing
- browse discovered directories under the scan root
- see which directories already have `.upload_dir`
- create new `.upload_dir` markers
- edit existing `.upload_dir` configuration
- delete `.upload_dir` markers

If a directory already has `.upload_dir`, it appears in the selected/upload set automatically inside the UI.

### Central

Central exposes the upload API and a small HTML landing page. It stages uploads to a temp location, atomically moves them into final storage, and keeps only the newest snapshots according to retention settings.

## Docker Layout

Packaging is separated per service:

- [central/Dockerfile](/d:/projects/relay_central/central/Dockerfile)
- [central/docker-compose.yml](/d:/projects/relay_central/central/docker-compose.yml)
- [edge/Dockerfile](/d:/projects/relay_central/edge/Dockerfile)
- [edge/docker-compose.yml](/d:/projects/relay_central/edge/docker-compose.yml)

That means users can run only the container they need:

Central:

```powershell
cd central
docker compose up --build
```

Edge:

```powershell
cd edge
docker compose up --build
```

## CI Layout

The build pipeline is also separated:

- [.github/workflows/build-central.yml](/d:/projects/relay_central/.github/workflows/build-central.yml)
- [.github/workflows/build-edge.yml](/d:/projects/relay_central/.github/workflows/build-edge.yml)

Each workflow builds only its own service and exports a downloadable OCI artifact.

## UI URLs

Default local URLs:

- Central UI: `http://localhost:8000/`
- Edge UI: `http://localhost:8080/`

## Repo Notes

- The old repo-level `.env.example` has been removed.
- The old `examples/` directory has been removed.
- Service-specific env examples live in [central/.env.example](/d:/projects/relay_central/central/.env.example) and [edge/.env.example](/d:/projects/relay_central/edge/.env.example).
- A root [.dockerignore](/d:/projects/relay_central/.dockerignore) is included for general repo hygiene.

## Known Limitations

- Central still uses a single shared bearer token for the POC.
- Only the local filesystem backend is implemented.
- Edge fingerprints trigger full snapshot uploads, not delta transfers.
- Docker Compose quiescing on Edge assumes the Docker socket and relevant Compose project paths are mounted into the Edge runtime.
- No formal automated end-to-end test suite is included yet.
