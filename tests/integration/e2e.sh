#!/usr/bin/env bash
set -euo pipefail

network="3to1go-e2e-$$"
central="3to1go-e2e-central-$$"
postgres="3to1go-e2e-postgres-$$"
edge="3to1go-e2e-edge-$$"
root="$(mktemp -d)"
cookie_central="$root/central.cookies"
cookie_edge="$root/edge.cookies"

cleanup() {
  local result=$?
  if [ "$result" -ne 0 ]; then
    for container in "$postgres" "$central" "$edge"; do
      if docker inspect "$container" >/dev/null 2>&1; then
        echo "--- $container startup diagnostics ---" >&2
        docker inspect --format '{{json .State}}' "$container" >&2 || true
        docker logs --tail 200 "$container" >&2 || true
        # CI uploads these full logs as an artifact.
        if [ -n "${E2E_LOG_DIR:-}" ]; then
          mkdir -p "$E2E_LOG_DIR"
          docker logs "$container" > "$E2E_LOG_DIR/${container%-$$}.log" 2>&1 || true
        fi
      fi
    done
  fi
  docker rm -f "$edge" "$central" "$postgres" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  docker run --rm -v "$root:/cleanup" alpine:3.21 sh -c 'rm -rf /cleanup/*' >/dev/null 2>&1 || true
  rm -rf "$root"
  return "$result"
}
trap cleanup EXIT

wait_for_service() {
  local container="$1"
  shift
  for _ in $(seq 1 60); do
    if [ "$(docker inspect --format '{{.State.Running}}' "$container")" != true ]; then
      echo "$container exited before becoming ready" >&2
      return 1
    fi
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "Timed out waiting for $container to become ready" >&2
  return 1
}

edge_login() {
  curl -fsS -c "$cookie_edge" -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"e2e-admin"}' \
    http://127.0.0.1:16556/api/session/login >/dev/null
}

# Prints the e2e job's directory entry, including its saved state.
edge_job() {
  curl -fsS -b "$cookie_edge" http://127.0.0.1:16556/api/directories | \
    jq -ec '.directories[] | select(.config.job_name == "e2e")'
}

# Runs one Edge backup cycle and waits for it to finish. Only call it on a
# freshly started Edge, whose last completed cycle is still empty.
edge_cycle() {
  curl -fsS -b "$cookie_edge" -X POST http://127.0.0.1:16556/api/run-now >/dev/null
  for _ in $(seq 1 60); do
    if curl -fsS -b "$cookie_edge" http://127.0.0.1:16556/api/status | \
      jq -e '.scheduler.last_completed_at != null' >/dev/null; then
      return 0
    fi
    sleep 1
  done
  echo "Timed out waiting for an Edge backup cycle" >&2
  return 1
}

restart_edge() {
  docker restart "$edge" >/dev/null
  wait_for_service "$edge" curl -fsS --connect-timeout 2 --max-time 3 http://127.0.0.1:16556/health
  edge_login
}

mkdir -p "$root/central-config" "$root/backups" "$root/staging" \
  "$root/edge-config" "$root/edge-state" "$root/edge-spool" "$root/scan"
printf 'job_name: e2e\n' > "$root/scan/.upload_dir"
# Enough files for Edge's changed-files check, which the restart test uses.
for i in $(seq -w 1 25); do
  printf '3to1go end-to-end payload %s\n' "$i" > "$root/scan/file-$i.txt"
done

docker network create "$network" >/dev/null
docker run -d --name "$postgres" --network "$network" \
  -e POSTGRES_DB=three_to_one_go \
  -e POSTGRES_USER=three_to_one_go \
  -e POSTGRES_PASSWORD=e2e-password \
  postgres:17-alpine >/dev/null

# The initialization server only listens on a Unix socket. Require TCP and a
# successful query so Central starts only after the final server is ready.
wait_for_service "$postgres" docker exec -e PGPASSWORD=e2e-password "$postgres" \
  psql -h 127.0.0.1 -U three_to_one_go -d three_to_one_go \
  -w -v ON_ERROR_STOP=1 -c 'SELECT 1'

docker run -d --name "$central" --network "$network" -p 16555:6555 \
  -e INDEX_DATABASE_URL="postgresql://three_to_one_go:e2e-password@$postgres:5432/three_to_one_go" \
  -e INITIAL_ADMIN_PASSWORD=admin \
  -e HTTP_HOST=0.0.0.0 -e HTTP_PORT=6555 \
  -e BACKUP_ROOT=/backups -e STAGING_DIR=/staging \
  -e SESSION_COOKIE_SECURE=false \
  -v "$root/central-config:/config" -v "$root/backups:/backups" \
  -v "$root/staging:/staging" \
  3to1go-central-validation >/dev/null

wait_for_service "$central" curl -fsS --connect-timeout 2 --max-time 3 http://127.0.0.1:16555/health

curl -fsS -c "$cookie_central" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' \
  http://127.0.0.1:16555/api/session/login >/dev/null
curl -fsS -b "$cookie_central" -H 'Content-Type: application/json' \
  -d '{"current_password":"admin","new_password":"e2e-admin","confirm_new_password":"e2e-admin"}' \
  http://127.0.0.1:16555/api/session/change-password >/dev/null
minted="$(curl -fsS -b "$cookie_central" -H 'Content-Type: application/json' \
  -d '{"shared":false}' http://127.0.0.1:16555/api/credentials/mint)"
credential="$(printf '%s' "$minted" | jq -er '.credential')"

docker run -d --name "$edge" --network "$network" -p 16556:6556 \
  -e CENTRAL_URL="http://$central:6555" -e EDGE_ID=e2e-edge \
  -e SCAN_ROOT=/scan \
  -e HTTP_HOST=0.0.0.0 -e HTTP_PORT=6556 \
  -e SESSION_COOKIE_SECURE=false \
  -v "$root/edge-config:/config" -v "$root/edge-state:/data/state" \
  -v "$root/edge-spool:/data/spool" -v "$root/scan:/scan" \
  3to1go-edge-validation >/dev/null

wait_for_service "$edge" curl -fsS --connect-timeout 2 --max-time 3 http://127.0.0.1:16556/health

curl -fsS -c "$cookie_edge" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' \
  http://127.0.0.1:16556/api/session/login >/dev/null
curl -fsS -b "$cookie_edge" -H 'Content-Type: application/json' \
  -d '{"current_password":"admin","new_password":"e2e-admin","confirm_new_password":"e2e-admin"}' \
  http://127.0.0.1:16556/api/session/change-password >/dev/null
settings_payload="$(curl -fsS -b "$cookie_edge" http://127.0.0.1:16556/api/settings | \
  jq -ec --arg credential "$credential" '.settings | objects | .edge_credential = $credential')"
curl -fsS -b "$cookie_edge" -H 'Content-Type: application/json' \
  -X POST -d "$settings_payload" http://127.0.0.1:16556/api/settings >/dev/null
curl -fsS -b "$cookie_edge" -X POST http://127.0.0.1:16556/api/run-now >/dev/null

instance="$(curl -fsS -b "$cookie_edge" http://127.0.0.1:16556/api/status | \
  jq -er '.edge_instance_id')"

for _ in $(seq 1 90); do
  if curl -fsS -H "Authorization: Bearer $credential" \
    -o "$root/recovered.snapshot" \
    "http://127.0.0.1:16555/backup/recovery/e2e-edge/$instance/e2e/latest"; then
    test -s "$root/recovered.snapshot"
    break
  fi
  sleep 2
done

if [ ! -s "$root/recovered.snapshot" ]; then
  echo "Timed out waiting for the Edge snapshot" >&2
  docker logs "$edge" >&2
  docker logs "$central" >&2
  exit 1
fi

# Keep the fingerprint Edge saved for this backup, for the restart checks below.
edge_login
fingerprint=""
for _ in $(seq 1 30); do
  fingerprint="$(edge_job | jq -r '.state.last_successful_fingerprint // empty')"
  [ -n "$fingerprint" ] && break
  sleep 1
done
if [ -z "$fingerprint" ]; then
  echo "Edge never saved the backup's fingerprint" >&2
  exit 1
fi

# Rehearse Central recovery with a logical dump while its filesystem is frozen.
# All destructive database operations below target this test's disposable DB.
docker stop "$edge" "$central" >/dev/null
docker exec "$postgres" sh -ec \
  'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -f /tmp/central-recovery.dump'
docker cp "$postgres:/tmp/central-recovery.dump" "$root/database.dump"
docker run --rm -v "$root:/recovery" alpine:3.21 sh -ec \
  'cd /recovery; tar -cpf deployment.tar central-config staging; tar -cpf snapshots.tar -C backups .'

docker exec "$postgres" sh -ec \
  'dropdb -U "$POSTGRES_USER" "$POSTGRES_DB"; createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
docker exec "$postgres" sh -ec \
  'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner --no-privileges --exit-on-error /tmp/central-recovery.dump'

# Move the old files aside so the server actually starts from restored copies.
docker run --rm -v "$root:/recovery" alpine:3.21 sh -ec \
  'cd /recovery; mv central-config central-config-original; mv staging staging-original; mv backups backups-original; mkdir backups; tar -xpf deployment.tar; tar -xpf snapshots.tar -C backups'
docker start "$central" >/dev/null
wait_for_service "$central" curl -fsS --connect-timeout 2 --max-time 3 \
  http://127.0.0.1:16555/health/ready

# A restored account and the original Edge token must work without replacement.
curl -fsS -c "$cookie_central" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"e2e-admin"}' \
  http://127.0.0.1:16555/api/session/login >/dev/null
curl -fsS -H "Authorization: Bearer $credential" \
  -o "$root/recovered-after-restore.snapshot" \
  "http://127.0.0.1:16555/backup/recovery/e2e-edge/$instance/e2e/latest"
cmp "$root/recovered.snapshot" "$root/recovered-after-restore.snapshot"

# Edge keeps its saved job state across a restart.
docker start "$edge" >/dev/null
wait_for_service "$edge" curl -fsS --connect-timeout 2 --max-time 3 http://127.0.0.1:16556/health
edge_login
edge_job | jq -e --arg fp "$fingerprint" '.state.last_successful_fingerprint == $fp' >/dev/null

# Renaming every file keeps the count but matches nothing from the last backup,
# so Edge holds the new archive for review instead of uploading it.
docker run --rm -v "$root/scan:/scan" alpine:3.21 sh -ec \
  'cd /scan; for f in file-*.txt; do mv "$f" "renamed-$f"; done'
edge_cycle
held="$(edge_job)"
if ! printf '%s' "$held" | jq -e '.state.last_status == "held_for_review" and (.state.pending_archive // "") != ""' >/dev/null; then
  echo "Edge did not hold the renamed backup: $held" >&2
  exit 1
fi
pending="$(printf '%s' "$held" | jq -r '.state.pending_archive')"

# The hold, its staged archive, and the last fingerprint survive a restart,
# and the next cycle still doesn't upload the held archive.
restart_edge
edge_job | jq -e --arg p "$pending" --arg fp "$fingerprint" \
  '.state.last_status == "held_for_review" and .state.pending_archive == $p and .state.last_successful_fingerprint == $fp' >/dev/null
test -s "$root/edge-spool/$(basename "$pending")"
edge_cycle
edge_job | jq -e '.state.last_status == "held_for_review"' >/dev/null
curl -fsS -H "Authorization: Bearer $credential" -o "$root/after-held-cycle.snapshot" \
  "http://127.0.0.1:16555/backup/recovery/e2e-edge/$instance/e2e/latest"
cmp "$root/recovered.snapshot" "$root/after-held-cycle.snapshot"

# Upload anyway approves the held archive, which then reaches Central.
curl -fsS -b "$cookie_edge" -H 'Content-Type: application/json' \
  -d '{"relative_path":"."}' http://127.0.0.1:16556/api/directories/force-send >/dev/null
approved=""
for _ in $(seq 1 60); do
  if edge_job | jq -e '.state.last_status == "success"' >/dev/null; then
    approved=yes
    break
  fi
  sleep 1
done
if [ -z "$approved" ]; then
  echo "Edge did not upload the approved archive: $(edge_job)" >&2
  exit 1
fi
curl -fsS -H "Authorization: Bearer $credential" -o "$root/approved.snapshot" \
  "http://127.0.0.1:16555/backup/recovery/e2e-edge/$instance/e2e/latest"
if cmp -s "$root/recovered.snapshot" "$root/approved.snapshot"; then
  echo "Central's latest snapshot is still the original after approval" >&2
  exit 1
fi
echo "Edge -> Central -> recovery, Central disaster-recovery, and Edge restart tests passed"
