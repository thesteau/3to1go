#!/bin/sh
# One-time migration: rename the PostgreSQL user and database
# from the old relay/relaycentral naming to 3to1go/3to1gocentral.
#
# Run this from deploy-example/central/ while the postgres container is running.
# Stop the Central app first so no active connections hold the database open.
#
# Typical usage:
#   docker compose stop central
#   sh migrate-db-credentials.sh
#   docker compose up -d central

set -e

PSQL="docker compose exec -T postgres psql -U postgres -d postgres"

# ── connectivity check ────────────────────────────────────────────────────────
echo "==> Connecting to postgres container..."
if ! $PSQL -c "SELECT 1;" >/dev/null 2>&1; then
  echo "ERROR: Cannot reach the postgres container."
  echo "       Make sure it is running:  docker compose up -d postgres"
  exit 1
fi
echo "    Connected."
echo ""

# ── detect old credentials ────────────────────────────────────────────────────
echo "==> Checking for old credentials (relay / relaycentral)..."
old_user=$($PSQL -tAc "SELECT COUNT(*)::int FROM pg_roles    WHERE rolname = 'relay'")
old_db=$($PSQL   -tAc "SELECT COUNT(*)::int FROM pg_database WHERE datname  = 'relaycentral'")

if [ "$old_user" = "0" ] && [ "$old_db" = "0" ]; then
  echo "    Nothing to migrate — old credentials not found."
  echo ""
  echo "If Central fails to start, confirm your .env contains:"
  echo "  POSTGRES_DB=3to1gocentral"
  echo "  POSTGRES_USER=3to1go"
  exit 0
fi

echo "    Old credentials detected."
echo ""

# ── rename user ───────────────────────────────────────────────────────────────
if [ "$old_user" != "0" ]; then
  printf "==> Renaming user:     relay          -> 3to1go        ... "
  $PSQL -c 'ALTER USER relay RENAME TO "3to1go";' >/dev/null 2>&1
  echo "ok"
fi

# ── rename database ───────────────────────────────────────────────────────────
if [ "$old_db" != "0" ]; then
  printf "==> Renaming database: relaycentral   -> 3to1gocentral ... "
  $PSQL -c 'ALTER DATABASE relaycentral RENAME TO "3to1gocentral";' >/dev/null 2>&1
  echo "ok"
fi

# ── transfer ownership ────────────────────────────────────────────────────────
printf "==> Setting owner:     3to1gocentral  -> 3to1go        ... "
$PSQL -c 'ALTER DATABASE "3to1gocentral" OWNER TO "3to1go";' >/dev/null 2>&1
echo "ok"

# ── done ─────────────────────────────────────────────────────────────────────
echo ""
echo "==> Migration complete."
echo ""
echo "Confirm your .env contains:"
echo "  POSTGRES_DB=3to1gocentral"
echo "  POSTGRES_USER=3to1go"
echo ""
echo "Then restart Central:"
echo "  docker compose up -d central"
