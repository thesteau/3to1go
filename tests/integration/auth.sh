#!/usr/bin/env bash
# Sourced by e2e.sh after Station's admin has changed its password.

# Explicitly require the inputs supplied by the parent end-to-end script.
: "${root:?auth.sh requires the end-to-end temporary directory}"
: "${cookie_station:?auth.sh requires the Station cookie jar}"
: "${postgres:?auth.sh requires the PostgreSQL container name}"

expect_status() {
  local expected="$1"
  shift
  local actual
  actual="$(curl -sS -o "$root/auth-response.json" -w '%{http_code}' "$@")"
  if [ "$actual" != "$expected" ]; then
    echo "Expected HTTP $expected, got $actual" >&2
    cat "$root/auth-response.json" >&2
    return 1
  fi
}

# An unused Station token can be listed and revoked without an instance.
unused="$(curl -fsS -b "$cookie_station" -X POST http://127.0.0.1:16555/api/credentials/mint | jq -er '.credential')"
unused_hash="$(printf '%s' "$unused" | sha256sum | cut -d ' ' -f 1)"
curl -fsS -b "$cookie_station" http://127.0.0.1:16555/api/credentials | jq -e --arg hash "$unused_hash" '.credentials | any(.token_hash == $hash)' >/dev/null
expect_status 200 -b "$cookie_station" -X DELETE "http://127.0.0.1:16555/api/credentials/$unused_hash"
expect_status 401 -H "Authorization: Bearer $unused" -H 'Content-Type: application/json' -d '{}' http://127.0.0.1:16555/backup/uploads/initiate

# Scope checks and metadata redaction use the same live API as the UI.
automation="$(curl -fsS -b "$cookie_station" -H 'Content-Type: application/json' -d '{"name":"e2e-reader","scopes":["read"],"ttl_days":1}' http://127.0.0.1:16555/api/automation-tokens)"
api_token="$(printf '%s' "$automation" | jq -er '.token')"
api_token_id="$(printf '%s' "$automation" | jq -er '.automation_token.id')"
curl -fsS -H "Authorization: Bearer $api_token" http://127.0.0.1:16555/api/overview | jq -e 'has("settings") | not' >/dev/null
expect_status 403 -H "Authorization: Bearer $api_token" -X POST http://127.0.0.1:16555/api/admin/uploads/pause
expect_status 403 -b "$cookie_station" -H 'Origin: http://127.0.0.1:16556' -H 'Sec-Fetch-Site: same-site' -X POST http://127.0.0.1:16555/api/admin/uploads/pause
expect_status 200 -b "$cookie_station" -X DELETE "http://127.0.0.1:16555/api/automation-tokens/$api_token_id"
expect_status 401 -H "Authorization: Bearer $api_token" http://127.0.0.1:16555/api/overview

expiring="$(curl -fsS -b "$cookie_station" -H 'Content-Type: application/json' -d '{"name":"e2e-expired","scopes":["read"],"ttl_days":1}' http://127.0.0.1:16555/api/automation-tokens)"
expired_id="$(printf '%s' "$expiring" | jq -er '.automation_token.id')"
expired_secret="$(printf '%s' "$expiring" | jq -er '.token')"
docker exec "$postgres" psql -U three_to_one_go -d three_to_one_go -c "UPDATE app_automation_tokens SET expires_at='2000-01-01T00:00:00Z' WHERE id='$expired_id'" >/dev/null
expect_status 401 -H "Authorization: Bearer $expired_secret" http://127.0.0.1:16555/api/overview

# Concurrent first uploads must never exceed the token's registration limit.
for scope in single shared; do
  limit=1
  options='{"shared":false}'
  if [ "$scope" = shared ]; then limit=2; options='{"shared":true,"max_registrations":2}'; fi
  binding_token="$(curl -fsS -b "$cookie_station" -H 'Content-Type: application/json' -d "$options" http://127.0.0.1:16555/api/credentials/mint | jq -er '.credential')"
  binding_hash="$(printf '%s' "$binding_token" | sha256sum | cut -d ' ' -f 1)"
  pids=()
  for i in $(seq 1 8); do
    payload="$(jq -nc --arg inst "$scope-$i" --arg key "auth-$scope-$i" '{scout_id:"auth-test",scout_instance_id:$inst,job_name:"job",fingerprint:"abcdef12",timestamp:"2026-01-01T00:00:00Z",archive_format:"tar.zst",archive_size_bytes:1,archive_sha256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",idempotency_key:$key}')"
    curl -sS -H "Authorization: Bearer $binding_token" -H 'Content-Type: application/json' -d "$payload" -o "$root/bind-$scope-$i.json" -w '%{http_code}' http://127.0.0.1:16555/backup/uploads/initiate > "$root/bind-$scope-$i.status" &
    pids+=("$!")
  done
  for pid in "${pids[@]}"; do wait "$pid"; done
  accepted=0
  upload_id=''
  for i in $(seq 1 8); do
    status="$(cat "$root/bind-$scope-$i.status")"
    if [ "$status" = 200 ]; then
      accepted=$((accepted+1))
      upload_id="$(jq -er '.upload_id' "$root/bind-$scope-$i.json")"
    elif [ "$status" != 403 ]; then
      cat "$root/bind-$scope-$i.json" >&2
      echo "Unexpected binding status $status" >&2
      return 1
    fi
  done
  test "$accepted" -eq "$limit"
  registered="$(docker exec "$postgres" psql -U three_to_one_go -d three_to_one_go -Atc "SELECT COUNT(*) FROM scout_registration WHERE credential_hash='$binding_hash'")"
  test "$registered" -eq "$limit"

  # A different valid token cannot write or finalize another token's session.
  other="$(curl -fsS -b "$cookie_station" -X POST http://127.0.0.1:16555/api/credentials/mint | jq -er '.credential')"
  expect_status 403 -H "Authorization: Bearer $other" -X PUT --data-binary 'x' "http://127.0.0.1:16555/backup/uploads/$upload_id/chunk?offset=0"
  expect_status 403 -H "Authorization: Bearer $other" -X POST "http://127.0.0.1:16555/backup/uploads/$upload_id/finalize"
  expect_status 200 -H "Authorization: Bearer $binding_token" -X PUT --data-binary 'x' "http://127.0.0.1:16555/backup/uploads/$upload_id/chunk?offset=0"
  expect_status 200 -b "$cookie_station" -X DELETE "http://127.0.0.1:16555/api/credentials/$binding_hash"
  registered="$(docker exec "$postgres" psql -U three_to_one_go -d three_to_one_go -Atc "SELECT COUNT(*) FROM scout_registration WHERE credential_hash='$binding_hash'")"
  test "$registered" -eq 0
done

# Different tokens racing for the same new instance cannot displace each other.
pids=()
claimants=()
for i in 1 2; do
  claimants+=("$(curl -fsS -b "$cookie_station" -X POST http://127.0.0.1:16555/api/credentials/mint | jq -er '.credential')")
done
for i in 1 2; do
  claimant="${claimants[$((i-1))]}"
  payload="$(jq -nc --arg key "claim-$i" '{scout_id:"auth-test",scout_instance_id:"contested",job_name:"job",fingerprint:"abcdef12",timestamp:"2026-01-01T00:00:00Z",archive_format:"tar.zst",archive_size_bytes:1,archive_sha256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",idempotency_key:$key}')"
  curl -sS -H "Authorization: Bearer $claimant" -H 'Content-Type: application/json' -d "$payload" -o "$root/claim-$i.json" -w '%{http_code}' http://127.0.0.1:16555/backup/uploads/initiate > "$root/claim-$i.status" &
  pids+=("$!")
done
for pid in "${pids[@]}"; do wait "$pid"; done
test "$(cat "$root/claim-1.status" "$root/claim-2.status")" = 200403 || test "$(cat "$root/claim-1.status" "$root/claim-2.status")" = 403200

# Two browsers: password changes keep the caller and invalidate the other.
curl -fsS -c "$root/other-browser.cookies" -H 'Content-Type: application/json' -d '{"username":"admin","password":"e2e-admin"}' http://127.0.0.1:16555/api/session/login >/dev/null
expect_status 200 -b "$cookie_station" -H 'Content-Type: application/json' -d '{"current_password":"e2e-admin","new_password":"e2e-admin","confirm_new_password":"e2e-admin"}' http://127.0.0.1:16555/api/session/change-password
expect_status 401 -b "$root/other-browser.cookies" http://127.0.0.1:16555/api/overview
expect_status 200 -b "$cookie_station" http://127.0.0.1:16555/api/overview
expect_status 200 -b "$cookie_station" -X POST http://127.0.0.1:16555/api/session/logout-all
expect_status 401 -b "$cookie_station" http://127.0.0.1:16555/api/overview
curl -fsS -c "$cookie_station" -H 'Content-Type: application/json' -d '{"username":"admin","password":"e2e-admin"}' http://127.0.0.1:16555/api/session/login >/dev/null

echo 'Authentication and concurrent Station token binding checks passed.'
