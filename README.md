# RelayCentralizer

RelayCentralizer is a two-service backup proof of concept:

- `central/` receives uploaded backup archives, stores them on disk, and applies retention.
- `edge/` discovers backup jobs under a scan root, creates `tar.zst` snapshots, and uploads them to Central.

The repo is split so each service can be built and run on its own.

## Repo Layout

- [`central/`](central/) - receiver API, storage, retention, and a small status UI
- [`edge/`](edge/) - backup agent, scheduler, upload logic, and job-management UI
- [`.github/workflows/docker-image.yml`](.github/workflows/docker-image.yml) - builds and pushes both Docker images

## Quick Start

1. Start Central:

   ```powershell
   cd central
   Copy-Item .env.example .env
   docker compose up --build
   ```

2. Start Edge in a second shell:

   ```powershell
   cd edge
   Copy-Item .env.example .env
   docker compose up --build
   ```

3. Before starting Edge, make sure `edge/.env` `AUTH_TOKEN` matches `central/.env` `AUTH_TOKEN`, and `edge/.env` `CENTRAL_URL` points at the Central service Edge can reach.

4. Open the UIs at `http://localhost:8000/` for Central and `http://localhost:8080/` for Edge.

From the Edge UI, create or edit `.upload_dir` markers under the scan root to define backup jobs.

## Where To Start

- Central setup and API details: [`central/README.md`](central/README.md)
- Edge setup, job format, and scheduler details: [`edge/README.md`](edge/README.md)

## Images And CI

The GitHub Actions workflow at [`.github/workflows/docker-image.yml`](.github/workflows/docker-image.yml) builds both service images from a single matrix job and pushes:

- `ghcr.io/<owner>/<repo>-relay-central:latest`
- `ghcr.io/<owner>/<repo>-relay-edge:latest`

Short-SHA tags are published alongside `latest`.
