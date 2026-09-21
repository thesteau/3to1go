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

mkdir -p "$root/central-config" "$root/backups" "$root/staging" \
  "$root/edge-config" "$root/edge-state" "$root/edge-spool" "$root/scan"
printf 'job_name: e2e\n' > "$root/scan/.upload_dir"
printf '3to1go end-to-end payload\n' > "$root/scan/payload.txt"

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
credential="$(printf '%s' "$minted" | python3 -c 'import json,sys; print(json.load(sys.stdin)["credential"])')"

docker run -d --name "$edge" --network "$network" -p 16556:6556 \
  -e CENTRAL_URL="http://$central:6555" -e EDGE_ID=e2e-edge \
  -e EDGE_CREDENTIAL="$credential" -e SCAN_ROOT=/scan \
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
  python3 -c 'import json,sys; p=json.load(sys.stdin)["settings"]; p["edge_credential"]="'"$credential"'"; print(json.dumps(p))')"
curl -fsS -b "$cookie_edge" -H 'Content-Type: application/json' \
  -X POST -d "$settings_payload" http://127.0.0.1:16556/api/settings >/dev/null
curl -fsS -b "$cookie_edge" -X POST http://127.0.0.1:16556/api/run-now >/dev/null

instance="$(curl -fsS -b "$cookie_edge" http://127.0.0.1:16556/api/status | \
  python3 -c 'import json,sys; print(json.load(sys.stdin)["edge_instance_id"])')"

for _ in $(seq 1 90); do
  if curl -fsS -H "Authorization: Bearer $credential" \
    -o "$root/recovered.snapshot" \
    "http://127.0.0.1:16555/backup/recovery/e2e-edge/$instance/e2e/latest"; then
    test -s "$root/recovered.snapshot"
    echo "Edge -> Central -> recovery end-to-end test passed"
    exit 0
  fi
  sleep 2
done

echo "Timed out waiting for the Edge snapshot" >&2
docker logs "$edge" >&2
docker logs "$central" >&2
exit 1
