#!/usr/bin/env bash
# Local end-to-end run: throwaway Postgres -> migrations -> Go server (mock AI/storage) -> Next build+start -> Playwright.
# Never touches production: everything binds to localhost and a disposable container. Tears everything down on exit.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
PG_NAME="griffin-e2e-pg-$$"
PG_PORT=55439
API_PORT=8081
WEB_PORT=3100
PIDS=()

cleanup() {
  local code=$?
  for pid in "${PIDS[@]:-}"; do [ -n "$pid" ] && kill "$pid" 2>/dev/null; done
  # next start spawns children; free the ports we own
  command -v fuser >/dev/null && { fuser -k "${WEB_PORT}/tcp" >/dev/null 2>&1; fuser -k "${API_PORT}/tcp" >/dev/null 2>&1; }
  docker rm -f "$PG_NAME" >/dev/null 2>&1
  rm -r "$WORK" 2>/dev/null
  exit $code
}
trap cleanup EXIT INT TERM

wait_for() { # url, label, tries
  for _ in $(seq 1 "${3:-60}"); do
    curl -fsS -o /dev/null "$1" 2>/dev/null && return 0
    sleep 1
  done
  echo "timeout waiting for $2 ($1)" >&2
  return 1
}

echo "==> postgres on :$PG_PORT"
docker run -d --rm --name "$PG_NAME" -e POSTGRES_PASSWORD=e2e -e POSTGRES_DB=griffin -p "127.0.0.1:${PG_PORT}:5432" postgres:16-alpine >/dev/null || exit 1
for _ in $(seq 1 40); do docker exec "$PG_NAME" pg_isready -h 127.0.0.1 -U postgres -d griffin >/dev/null 2>&1 && break; sleep 1; done
sleep 2
export DATABASE_URL="postgres://postgres:e2e@127.0.0.1:${PG_PORT}/griffin?sslmode=disable"

echo "==> migrations"
(cd "$ROOT" && ENVIRONMENT=development go run ./cmd/griffin-migrate -local-auth-shim up) || exit 1

echo "==> go server on :$API_PORT"
(cd "$ROOT" && go build -o "$WORK/griffin-server" ./cmd/griffin-server) || exit 1
ENVIRONMENT=development ENABLE_DEV_LOGIN=true AI_PROVIDER=mock STORAGE_PROVIDER=mock \
  SCALE_TO_ZERO_ENABLED=false CORS_ALLOWED_ORIGINS="http://localhost:${WEB_PORT}" \
  PORT="$API_PORT" GRIFFIN_MODE=all WORKSPACE_ROOT="$WORK/workspaces" \
  SUPABASE_JWT_SECRET="e2e-$(head -c 24 /dev/urandom | base64 | tr -dc A-Za-z0-9)" \
  WORKER_API_KEY="e2e-$(head -c 24 /dev/urandom | base64 | tr -dc A-Za-z0-9)" \
  "$WORK/griffin-server" >"$WORK/server.log" 2>&1 &
PIDS+=($!)
wait_for "http://localhost:${API_PORT}/healthz" "go server" 40 || { tail -20 "$WORK/server.log"; exit 1; }

echo "==> next build + start on :$WEB_PORT"
cd "$ROOT/web" || exit 1
export NEXT_PUBLIC_API_URL="http://localhost:${API_PORT}" NEXT_PUBLIC_ENABLE_DEV_LOGIN=true
NEXT_DIST_DIR=.next-e2e npx next build >"$WORK/build.log" 2>&1 || { tail -30 "$WORK/build.log"; exit 1; }
NEXT_DIST_DIR=.next-e2e npx next start -p "$WEB_PORT" >"$WORK/web.log" 2>&1 &
PIDS+=($!)
wait_for "http://localhost:${WEB_PORT}/login" "next start" 40 || { tail -20 "$WORK/web.log"; exit 1; }

echo "==> playwright"
E2E_BASE="http://localhost:${WEB_PORT}" node e2e/run.mjs
RESULT=$?
rm -r "$ROOT/web/.next-e2e" 2>/dev/null
exit $RESULT
