#!/usr/bin/env bash
# Builds the PWA against the mock API URL, serves it, runs scripts/ui-tasks.mjs. Args pass through.
# Usage: scripts/ui-tasks.sh [--shots DIR]
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
PORT=3301
DIST=.next-uitasks
cleanup() { [ -n "${SERVER_PID:-}" ] && kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$DIST"; }
trap cleanup EXIT
export NEXT_DIST_DIR=$DIST NEXT_PUBLIC_ENABLE_DEV_LOGIN=true NEXT_PUBLIC_API_URL=http://localhost:9999
npx next build >/dev/null
npx next start -p $PORT >/dev/null 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 40); do curl -sf -o /dev/null "http://localhost:$PORT/login" && break; sleep 1; done
node scripts/ui-tasks.mjs --base "http://localhost:$PORT" "$@"
