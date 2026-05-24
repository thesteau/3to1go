# RelayCentralizer

RelayCentralizer is a simple backup system with two parts:

- `Edge` runs on the machine that has the files you care about.
- `Central` receives encrypted backups and keeps them on disk.

If you want the shortest mental model:

1. Pick a machine to run Central.
2. Run Edge on each machine you want to back up.
3. Mark folders on Edge with a `.upload_dir` file.
4. Edge packs and encrypts those folders.
5. Central stores the snapshots and lets you browse them in a web UI.

## What It Feels Like To Use

You do not write a giant backup config file up front.

Instead, Edge scans a root folder such as `/home`, `/Users`, or `C:\Users`. Any directory that contains a `.upload_dir` file becomes a backup job. That makes setup feel more like "drop a marker into the folder I want backed up" than "build a big central manifest."

## How The System Works

The full flow looks like this:

1. You create a `.upload_dir` file in a folder on Edge.
2. Edge notices that folder during its scan.
3. Edge fingerprints the files to see whether anything changed.
4. If something changed, Edge creates a `tar.zst` archive.
5. Edge encrypts that archive locally.
6. Edge uploads the encrypted archive to Central.
7. Central verifies the upload, stores it by `edge_id/job_name`, and prunes old snapshots.
8. You browse and download snapshots from Central's web UI.

Central never needs your plaintext files in order to store them. Decryption happens in the browser when you download an encrypted snapshot.

## Quick Start

### 1. Start Central

Central is usually the always-on receiver.

- Use [`deploy-example/central/`](deploy-example/central/) if you want a Docker Compose starting point.
- Create the auth token file Central expects.
- Start the container.
- Open the Central UI.

More detail: [`central/README.md`](central/README.md)

### 2. Start Edge

Edge runs on the machine that owns the files.

- Start Edge on the host or in Docker.
- Open the local Edge UI at `http://localhost:8080/`.
- Set `CENTRAL_URL`.
- Enter the same auth token Central uses.
- Pick a unique `EDGE_ID`.

More detail: [`edge/README.md`](edge/README.md)

### 3. Mark A Folder For Backup

Create a file named `.upload_dir` inside the folder you want to back up.

Example:

```text
/scan/photos/.upload_dir
```

Minimal example:

```yaml
job_name: photos
```

An empty `.upload_dir` also works. In that case, Edge uses the folder name as the job name.

## Important Things To Know

### Encryption

Each Edge creates its own `encryption.key` file on first run.

- Edge encrypts archives before upload.
- Central stores encrypted blobs.
- Central's UI can verify the key fingerprint before decrypting a download.
- If you lose `encryption.key`, old encrypted snapshots from that Edge are not recoverable.

Back up that key file.

### Auth Token

Central and Edge share one bearer token.

- The token must match on Central and every Edge that talks to it.
- Central manages only its own token file.
- Edge stores its token in its own local settings.
- Rotation is global: if you change the token, you need to update every connected Edge.

### Unique Edge IDs Matter

Each Edge needs its own `EDGE_ID`.

Central now reserves each `EDGE_ID` to one stable Edge instance. That prevents two machines from silently writing into the same namespace and pruning each other's snapshots.

### Storage Scope

Central stores backups on the local filesystem. It is not trying to be an S3, Dropbox, or Google Drive sync engine.

If you want off-site copies, the expected pattern is:

1. Let RelayCentralizer write to Central's local `BACKUP_ROOT`.
2. Use a separate sync or replication tool to copy that storage elsewhere.

## Repo Layout

- [`central/`](central/) - receiver API, storage logic, and Central web UI
- [`edge/`](edge/) - scan agent, upload logic, encryption, and Edge web UI
- [`deploy-example/central/`](deploy-example/central/) - sample Central Compose setup
- [`deploy-example/edge/`](deploy-example/edge/) - sample Edge Compose setup

## Which README Should I Read Next?

- Read [`central/README.md`](central/README.md) if you are setting up the receiving server.
- Read [`edge/README.md`](edge/README.md) if you are setting up a machine that will produce backups.
