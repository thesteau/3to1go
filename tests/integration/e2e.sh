#!/usr/bin/env bash
set -euo pipefail

network="3to1go-e2e-$$"
station="3to1go-e2e-station-$$"
postgres="3to1go-e2e-postgres-$$"
scout="3to1go-e2e-scout-$$"
root="$(mktemp -d)"
cookie_station="$root/station.cookies"
cookie_scout="$root/scout.cookies"

cleanup() {
  local result=$?
  if [ "$result" -ne 0 ]; then
    for container in "$postgres" "$station" "$scout"; do
      if docker inspect "$container" >/dev/null 2>&1; then
        echo "--- $container startup diagnostics ---" >&2
        docker inspect --format '{{json .State}}' "$container" >&2 || true
        docker logs --tail 200 "$container" >&2 || true
        # CI uploads these full logs as an artifact.
        if [ -n "${E2E_LOG_DIR:-}" ]; then
          mkdir -p "$E2E_LOG_DIR"
          docker logs "$container" > "$E2E_LOG_DIR/${container%-"$$"}.log" 2>&1 || true
        fi
      fi
    done
  fi
  docker rm -f "$scout" "$station" "$postgres" >/dev/null 2>&1 || true
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

scout_login() {
  curl -fsS -c "$cookie_scout" -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"e2e-admin"}' \
    http://127.0.0.1:16556/api/session/login >/dev/null
}

# Prints the e2e job's directory entry, including its saved state.
scout_job() {
  curl -fsS -b "$cookie_scout" http://127.0.0.1:16556/api/directories | \
    jq -ec '.directories[] | select(.config.job_name == "e2e")'
}

# Runs one Scout backup cycle and waits for it to finish. Only call it on a
# freshly started Scout, whose last completed cycle is still empty.
scout_cycle() {
  curl -fsS -b "$cookie_scout" -X POST http://127.0.0.1:16556/api/run-now >/dev/null
  for _ in $(seq 1 60); do
    if curl -fsS -b "$cookie_scout" http://127.0.0.1:16556/api/status | \
      jq -e '.scheduler.last_completed_at != null' >/dev/null; then
      return 0
    fi
    sleep 1
  done
  echo "Timed out waiting for a Scout backup cycle" >&2
  return 1
}

restart_scout() {
  docker restart "$scout" >/dev/null
  wait_for_service "$scout" curl -fsS --connect-timeout 2 --max-time 3 http://127.0.0.1:16556/health
  scout_login
}

mkdir -p "$root/station-config" "$root/backups" "$root/staging" \
  "$root/scout-config" "$root/scout-state" "$root/scout-spool" "$root/scan"
printf 'job_name: e2e\n' > "$root/scan/.upload_dir"
# Enough files for Scout's changed-files check, which the restart test uses.
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
# successful query so Station starts only after the final server is ready.
wait_for_service "$postgres" docker exec -e PGPASSWORD=e2e-password "$postgres" \
  psql -h 127.0.0.1 -U three_to_one_go -d three_to_one_go \
  -w -v ON_ERROR_STOP=1 -c 'SELECT 1'

docker run -d --name "$station" --network "$network" -p 16555:6555 \
  -e INDEX_DATABASE_URL="postgresql://three_to_one_go:e2e-password@$postgres:5432/three_to_one_go" \
  -e INITIAL_ADMIN_PASSWORD=admin \
  -e HTTP_HOST=0.0.0.0 -e HTTP_PORT=6555 \
  -e BACKUP_ROOT=/backups -e STAGING_DIR=/staging \
  -e SESSION_COOKIE_SECURE=false \
  -v "$root/station-config:/config" -v "$root/backups:/backups" \
  -v "$root/staging:/staging" \
  3to1go-station-validation >/dev/null

wait_for_service "$station" curl -fsS --connect-timeout 2 --max-time 3 http://127.0.0.1:16555/health

curl -fsS -c "$cookie_station" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' \
  http://127.0.0.1:16555/api/session/login >/dev/null
curl -fsS -b "$cookie_station" -H 'Content-Type: application/json' \
  -d '{"current_password":"admin","new_password":"e2e-admin","confirm_new_password":"e2e-admin"}' \
  http://127.0.0.1:16555/api/session/change-password >/dev/null
# shellcheck source=tests/integration/auth.sh
source "$(dirname "${BASH_SOURCE[0]}")/auth.sh"

minted="$(curl -fsS -b "$cookie_station" -H 'Content-Type: application/json' \
  -d '{"shared":false}' http://127.0.0.1:16555/api/credentials/mint)"
credential="$(printf '%s' "$minted" | jq -er '.credential')"

docker run -d --name "$scout" --network "$network" -p 16556:6556 \
  -e STATION_URL="http://$station:6555" -e SCOUT_ID=e2e-scout \
  -e SCAN_ROOT=/scan \
  -e HTTP_HOST=0.0.0.0 -e HTTP_PORT=6556 \
  -e SESSION_COOKIE_SECURE=false \
  -v "$root/scout-config:/config" -v "$root/scout-state:/data/state" \
  -v "$root/scout-spool:/data/spool" -v "$root/scan:/scan" \
  3to1go-scout-validation >/dev/null

wait_for_service "$scout" curl -fsS --connect-timeout 2 --max-time 3 http://127.0.0.1:16556/health

curl -fsS -c "$cookie_scout" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' \
  http://127.0.0.1:16556/api/session/login >/dev/null
curl -fsS -b "$cookie_scout" -H 'Content-Type: application/json' \
  -d '{"current_password":"admin","new_password":"e2e-admin","confirm_new_password":"e2e-admin"}' \
  http://127.0.0.1:16556/api/session/change-password >/dev/null
settings_payload="$(curl -fsS -b "$cookie_scout" http://127.0.0.1:16556/api/settings | \
  jq -ec --arg credential "$credential" '.settings | objects | .scout_credential = $credential')"
curl -fsS -b "$cookie_scout" -H 'Content-Type: application/json' \
  -X POST -d "$settings_payload" http://127.0.0.1:16556/api/settings >/dev/null
# A Scout automation token uses its SQLite store, without leaking settings.
scout_automation="$(curl -fsS -b "$cookie_scout" -H 'Content-Type: application/json' -d '{"name":"e2e-backup","scopes":["read","backup"],"ttl_days":1}' http://127.0.0.1:16556/api/automation-tokens)"
scout_api_token="$(printf '%s' "$scout_automation" | jq -er '.token')"
scout_api_token_id="$(printf '%s' "$scout_automation" | jq -er '.automation_token.id')"
curl -fsS -H "Authorization: Bearer $scout_api_token" http://127.0.0.1:16556/api/status | jq -e 'has("settings") | not' >/dev/null
expect_status 403 -H "Authorization: Bearer $scout_api_token" http://127.0.0.1:16556/api/encryption-key
curl -fsS -H "Authorization: Bearer $scout_api_token" -X POST http://127.0.0.1:16556/api/run-now >/dev/null
expect_status 200 -b "$cookie_scout" -X DELETE "http://127.0.0.1:16556/api/automation-tokens/$scout_api_token_id"
expect_status 401 -H "Authorization: Bearer $scout_api_token" http://127.0.0.1:16556/api/status

instance="$(curl -fsS -b "$cookie_scout" http://127.0.0.1:16556/api/status | \
  jq -er '.scout_instance_id')"

for _ in $(seq 1 90); do
  if curl -fsS -H "Authorization: Bearer $credential" \
    -o "$root/recovered.snapshot" \
    "http://127.0.0.1:16555/backup/recovery/e2e-scout/$instance/e2e/latest"; then
    test -s "$root/recovered.snapshot"
    break
  fi
  sleep 2
done

if [ ! -s "$root/recovered.snapshot" ]; then
  echo "Timed out waiting for the Scout snapshot" >&2
  docker logs "$scout" >&2
  docker logs "$station" >&2
  exit 1
fi

# Keep the fingerprint Scout saved for this backup, for the restart checks below.
scout_login
fingerprint=""
for _ in $(seq 1 30); do
  fingerprint="$(scout_job | jq -r '.state.last_successful_fingerprint // empty')"
  [ -n "$fingerprint" ] && break
  sleep 1
done
if [ -z "$fingerprint" ]; then
  echo "Scout never saved the backup's fingerprint" >&2
  exit 1
fi

# Rehearse Station recovery with a logical dump while its filesystem is frozen.
# All destructive database operations below target this test's disposable DB.
docker stop "$scout" "$station" >/dev/null
docker exec "$postgres" sh -ec \
  'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -f /tmp/station-recovery.dump'
docker cp "$postgres:/tmp/station-recovery.dump" "$root/database.dump"
docker run --rm -v "$root:/recovery" alpine:3.21 sh -ec \
  'cd /recovery; tar -cpf deployment.tar station-config staging; tar -cpf snapshots.tar -C backups .'

docker exec "$postgres" sh -ec \
  'dropdb -U "$POSTGRES_USER" "$POSTGRES_DB"; createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
docker exec "$postgres" sh -ec \
  'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner --no-privileges --exit-on-error /tmp/station-recovery.dump'

# Move the old files aside so the server actually starts from restored copies.
docker run --rm -v "$root:/recovery" alpine:3.21 sh -ec \
  'cd /recovery; mv station-config station-config-original; mv staging staging-original; mv backups backups-original; mkdir backups; tar -xpf deployment.tar; tar -xpf snapshots.tar -C backups'
docker start "$station" >/dev/null
wait_for_service "$station" curl -fsS --connect-timeout 2 --max-time 3 \
  http://127.0.0.1:16555/health/ready

# A restored account and the original Scout token must work without replacement.
curl -fsS -c "$cookie_station" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"e2e-admin"}' \
  http://127.0.0.1:16555/api/session/login >/dev/null
curl -fsS -H "Authorization: Bearer $credential" \
  -o "$root/recovered-after-restore.snapshot" \
  "http://127.0.0.1:16555/backup/recovery/e2e-scout/$instance/e2e/latest"
cmp "$root/recovered.snapshot" "$root/recovered-after-restore.snapshot"

# Scout keeps its saved job state across a restart.
docker start "$scout" >/dev/null
wait_for_service "$scout" curl -fsS --connect-timeout 2 --max-time 3 http://127.0.0.1:16556/health
scout_login
scout_job | jq -e --arg fp "$fingerprint" '.state.last_successful_fingerprint == $fp' >/dev/null

# Renaming every file keeps the count but matches nothing from the last backup,
# so Scout holds the new archive for review instead of uploading it.
docker run --rm -v "$root/scan:/scan" alpine:3.21 sh -ec \
  'cd /scan; for f in file-*.txt; do mv "$f" "renamed-$f"; done'
scout_cycle
held="$(scout_job)"
if ! printf '%s' "$held" | jq -e '.state.last_status == "held_for_review" and (.state.pending_archive // "") != ""' >/dev/null; then
  echo "Scout did not hold the renamed backup: $held" >&2
  exit 1
fi
pending="$(printf '%s' "$held" | jq -r '.state.pending_archive')"

# The hold, its staged archive, and the last fingerprint survive a restart,
# and the next cycle still doesn't upload the held archive.
restart_scout
scout_job | jq -e --arg p "$pending" --arg fp "$fingerprint" \
  '.state.last_status == "held_for_review" and .state.pending_archive == $p and .state.last_successful_fingerprint == $fp' >/dev/null
test -s "$root/scout-spool/$(basename "$pending")"
scout_cycle
scout_job | jq -e '.state.last_status == "held_for_review"' >/dev/null
curl -fsS -H "Authorization: Bearer $credential" -o "$root/after-held-cycle.snapshot" \
  "http://127.0.0.1:16555/backup/recovery/e2e-scout/$instance/e2e/latest"
cmp "$root/recovered.snapshot" "$root/after-held-cycle.snapshot"

# Upload anyway approves the held archive, which then reaches Station.
curl -fsS -b "$cookie_scout" -H 'Content-Type: application/json' \
  -d '{"relative_path":"."}' http://127.0.0.1:16556/api/directories/force-send >/dev/null
approved=""
for _ in $(seq 1 60); do
  if scout_job | jq -e '.state.last_status == "success"' >/dev/null; then
    approved=yes
    break
  fi
  sleep 1
done
if [ -z "$approved" ]; then
  echo "Scout did not upload the approved archive: $(scout_job)" >&2
  exit 1
fi
curl -fsS -H "Authorization: Bearer $credential" -o "$root/approved.snapshot" \
  "http://127.0.0.1:16555/backup/recovery/e2e-scout/$instance/e2e/latest"
if cmp -s "$root/recovered.snapshot" "$root/approved.snapshot"; then
  echo "Station's latest snapshot is still the original after approval" >&2
  exit 1
fi
echo "Scout -> Station -> recovery, Station disaster-recovery, and Scout restart tests passed"
