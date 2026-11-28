#!/bin/bash
# Usage: scripts/e2e/up.sh  (needs docker and GEMINI_KEY or GEMINI_API_KEY in .env); stop: docker stop griffin-e2e-pg; pkill -f "$E2E_DIR/griffin-server"
# Local end-to-end harness: throwaway Postgres + migrations + one test owner + griffin-server on :18080 with a local
# HS256 login and the real Gemini key. Prints a bearer token file path. Touches nothing in production.
set -euo pipefail
E2E=${E2E_DIR:-/tmp/griffin-e2e}; mkdir -p "$E2E"
REPO=$(cd "$(dirname "$0")/../.." && pwd)
DB=postgres://postgres:postgres@127.0.0.1:55440/griffin_e2e?sslmode=disable
USER_ID=11111111-1111-4111-8111-111111111111
EMAIL=owner@e2e.invalid
SECRET=e2e-local-secret-not-for-production-0123456789

if ! docker ps --format '{{.Names}}' | grep -qx griffin-e2e-pg; then
  docker run -d --rm --name griffin-e2e-pg -e POSTGRES_PASSWORD=postgres -p 127.0.0.1:55440:5432 postgres:17-alpine >/dev/null
  until docker exec griffin-e2e-pg pg_isready -U postgres >/dev/null 2>&1; do sleep 0.5; done
  sleep 1
  docker exec griffin-e2e-pg psql -U postgres -qc "create database griffin_e2e"
  cd "$REPO" && ENVIRONMENT=development MIGRATION_DATABASE_URL="$DB" go run ./cmd/griffin-migrate -local-auth-shim up | tail -1
  docker exec griffin-e2e-pg psql -U postgres -d griffin_e2e -qc "insert into auth.users (id, email) values ('$USER_ID', '$EMAIL') on conflict do nothing"
  docker exec griffin-e2e-pg psql -U postgres -d griffin_e2e -qc "insert into profiles (id, email) values ('$USER_ID', '$EMAIL') on conflict do nothing" || true
fi

cd "$REPO"
set -a; . ./.env; set +a
export GEMINI_API_KEY="${GEMINI_API_KEY:-${GEMINI_KEY:-}}"
pkill -f "$E2E/griffin-server" 2>/dev/null || true
go build -o "$E2E/griffin-server" ./cmd/griffin-server
env -i HOME="$HOME" PATH="$PATH" ENVIRONMENT=development GRIFFIN_MODE=api PORT=18080 DATABASE_URL="$DB" \
  SUPABASE_JWT_SECRET="$SECRET" ALLOWED_USER_EMAILS="$EMAIL" GEMINI_API_KEY="$GEMINI_API_KEY" \
  WORKER_API_KEY=e2e-worker-key-0123456789abcdef0123456789 ENABLE_DEV_LOGIN=true HOSTS_ENABLED=true SCALE_TO_ZERO_ENABLED=false \
  "$E2E/griffin-server" > "$E2E/server.log" 2>&1 &
for i in $(seq 1 60); do curl -sf http://127.0.0.1:18080/healthz >/dev/null && break; sleep 0.5; done

curl -sf -X POST http://127.0.0.1:18080/api/v1/auth/login -H "Content-Type: application/json" -d "{\"email\":\"$EMAIL\"}" | python3 -c "import json,sys;d=json.load(sys.stdin);print((d.get('data') or d)['token'])" > "$E2E/token"
curl -sf http://127.0.0.1:18080/healthz >/dev/null && echo "server up on :18080; token in $E2E/token" || { echo "server failed"; tail -20 "$E2E/server.log"; exit 1; }
