# RelayCentralizer

RelayCentralizer is a two-image backup workflow:

- `central/` is the receiving service. It accepts snapshot uploads from Edge, stages them safely, stores them by `edge_id/job_name`, and applies retention.
- `edge/` is the device-side agent. It scans a configured root for `.upload_dir` markers, builds `tar.zst` archives for changed jobs, and uploads them to Central.

The two images are meant to run separately:

- Central runs wherever you want backups collected and retained.
- Edge runs on each device or host that should produce backups.

## End-To-End Flow

1. An operator creates a `.upload_dir` file in a directory under the Edge scan root.
2. Edge discovers that directory as a backup job.
3. Edge builds a fingerprint of the selected files.
4. If nothing changed since the last successful upload, the job is skipped.
5. If the job defines optional `docker_compose` quiesce settings, Edge can stop the target services before the archive is created and start them again afterward.
6. Edge uploads the archive to Central.
7. Central stages the upload, commits it into the backup store, and prunes older snapshots for that job.

## Repo Layout

- [`central/`](central/) - receiver API, storage, retention, and Central status UI
- [`edge/`](edge/) - scan agent, scheduler, upload pipeline, quiesce support, and Edge job-management UI
- [`.github/workflows/docker-image.yml`](.github/workflows/docker-image.yml) - builds and pushes both Docker images

## Running The Services

You can run each service with its Docker image, directly with Python, or with the bundled `docker-compose.yml` files during local development.

For local development only:

1. Start Central from [`central/`](central/).
2. Start Edge from [`edge/`](edge/).
3. Set the same `AUTH_TOKEN` in both env files.
4. Point Edge `CENTRAL_URL` at the Central service it can reach.

The bundled compose files are convenience wrappers for local setup, not the core backup workflow.

## Where To Start

- Central setup and API details: [`central/README.md`](central/README.md)
- Edge setup, job format, scheduler behavior, and quiesce flow: [`edge/README.md`](edge/README.md)

## Images And CI

The GitHub Actions workflow at [`.github/workflows/docker-image.yml`](.github/workflows/docker-image.yml) builds both service images from a single matrix job and pushes:

- `ghcr.io/<owner>/<repo>-relay-central:latest`
- `ghcr.io/<owner>/<repo>-relay-edge:latest`

Short-SHA tags are published alongside `latest`.
