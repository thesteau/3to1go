#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
MANAGER="$SCRIPT_DIR/scripts/dev_edge.py"

if command -v python3 >/dev/null 2>&1; then
  exec python3 "$MANAGER" "$@"
fi

if command -v python >/dev/null 2>&1; then
  exec python "$MANAGER" "$@"
fi

echo "Python 3 was not found on PATH. Install Python and try again." >&2
exit 1
