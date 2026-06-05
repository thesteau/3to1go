#!/bin/sh
# Verify that critical Edge data files are present in the bind-mounted volumes.
# Run this from deploy-example/edge/ (or any per-edge deployment folder that
# mirrors this layout) before restarting the Edge container after an update.
#
# The container does NOT need to be running — this checks the host-side paths
# that are bind-mounted into the container.
#
# Usage:
#   sh verify-edge-data.sh

ok=0
warn=0

pass() { echo "  [ok]     $1"; ok=$((ok + 1)); }
miss() { echo "  [MISS]   $1  <-- $2"; warn=$((warn + 1)); }

check_file() {
  if [ -f "$1" ]; then pass "$1"; else miss "$1" "$2"; fi
}

check_dir() {
  if [ -d "$1" ]; then
    count=$(ls "$1" 2>/dev/null | wc -l | tr -d ' ')
    pass "$1  ($count item(s))"
  else
    miss "$1" "$2"
  fi
}

echo ""
echo "==> Edge data verification"
echo "    Checking bind-mount paths relative to:  $(pwd)"
echo ""

echo "── Critical (loss = encrypted backups unrecoverable) ──────────────────────"
check_file "./config/encryption.key" \
  "encryption key is missing — old snapshots cannot be decrypted without it"
check_file "./config/installation.id" \
  "installation ID is missing — Edge will generate a new one on next start"
echo ""

echo "── Database ────────────────────────────────────────────────────────────────"
found_db=0
for f in ./config/*.db; do
  [ -f "$f" ] || continue
  sz=$(wc -c < "$f" | tr -d ' ')
  pass "$f  (${sz} bytes)"
  found_db=1
done
if [ "$found_db" = "0" ]; then
  echo "  [note]   no .db files found — Edge creates the database on first start"
fi
echo ""

echo "── State ───────────────────────────────────────────────────────────────────"
check_file "./state/edge-state.json" \
  "job state is missing — Edge will rebuild from scratch on next scan (no data loss)"
echo ""

echo "── Spool (in-flight archives) ──────────────────────────────────────────────"
if [ -d "./spool" ]; then
  count=$(ls ./spool 2>/dev/null | wc -l | tr -d ' ')
  if [ "$count" -gt 0 ]; then
    pass "./spool  ($count pending item(s) — these will be retried on next start)"
  else
    pass "./spool  (empty — no pending uploads)"
  fi
else
  echo "  [note]   ./spool does not exist yet — Edge creates it on first start"
fi
echo ""

echo "── Certificates ────────────────────────────────────────────────────────────"
check_dir "./config/trusted-certs" \
  "certs dir is missing — Edge creates it on first start"
echo ""

echo "═══════════════════════════════════════════════════════════════════════════"
if [ "$warn" -gt 0 ]; then
  echo "  $ok check(s) passed, $warn MISSING."
  echo ""
  echo "  The encryption key is the only truly irreplaceable file."
  echo "  Everything else is recreated automatically on next start."
else
  echo "  All $ok check(s) passed. Data is intact."
fi
echo ""
