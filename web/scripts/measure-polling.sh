#!/usr/bin/env bash
# Builds the PWA (mock API URL, dev login on so the mock token is accepted), serves it, and runs
# scripts/measure-polling.mjs for one scenario. Extra args are passed through to the harness.
# Usage: scripts/measure-polling.sh [--seconds 60] [--scenario active|completes|failing|hidden|chat|chat-failed|chat-404|chat-objective] [--shots DIR]
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

PORT=3300
DIST=.next-measure
cleanup() { [ -n "${SERVER_PID:-}" ] && kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$DIST"; }
trap cleanup EXIT

NEXT_DIST_DIR=$DIST NEXT_PUBLIC_ENABLE_DEV_LOGIN=true NEXT_PUBLIC_API_URL=http://localhost:9999 npx next build >/dev/null
NEXT_DIST_DIR=$DIST NEXT_PUBLIC_ENABLE_DEV_LOGIN=true NEXT_PUBLIC_API_URL=http://localhost:9999 npx next start -p $PORT >/dev/null 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 40); do curl -sf -o /dev/null "http://localhost:$PORT/login" && break; sleep 1; done

node scripts/measure-polling.mjs --base "http://localhost:$PORT" "$@"
