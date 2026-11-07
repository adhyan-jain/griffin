#!/usr/bin/env bash
# Runs inside the AWS CodeBuild project "griffin-redeploy" (infra/aws/redeploy.yaml), or on any machine
# with the same credentials. It never creates infrastructure and never prints configuration.
#
# Env:  MODE=plan|apply     plan (default) changes nothing and prints what apply would do
#       TARGET=all|lambda|worker|none     none = migrations only
#       MIGRATE=yes|no      apply database migrations first (default yes)
#       DATABASE_SECRET_ARN Secrets Manager secret holding JSON {"url": "<session-pooler or direct URL>"}
#       MIGRATION_DATABASE_URL  if already set, used instead of the secret (local testing)
#
# Order matters (ADR-077): the schema is expanded BEFORE the new binaries start, because the server
# refuses to start on a schema that is behind. Migrations here only add tables and indexes, so the
# old binaries keep working until the new ones are deployed.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

MODE="${MODE:-plan}"; TARGET="${TARGET:-all}"; MIGRATE="${MIGRATE:-yes}"
case "$MODE" in plan|apply) ;; *) echo "MODE must be plan or apply, got '$MODE'" >&2; exit 2;; esac
case "$TARGET" in all|lambda|worker|none) ;; *) echo "TARGET must be all, lambda, worker or none, got '$TARGET'" >&2; exit 2;; esac
case "$MIGRATE" in yes|no) ;; *) echo "MIGRATE must be yes or no, got '$MIGRATE'" >&2; exit 2;; esac

SHA="$(git rev-parse --short HEAD)"
APPLY_FLAG=""; [ "$MODE" = apply ] && APPLY_FLAG="--apply"
echo "=============================================================="
echo " griffin redeploy   commit $SHA   mode=$MODE   target=$TARGET   migrate=$MIGRATE"
[ "$MODE" = plan ] && echo " PLAN ONLY: nothing below changes anything. Start the build again with MODE=apply to deploy."
echo "=============================================================="

if [ "$MIGRATE" = yes ]; then
  echo; echo "== database migrations"
  if [ -z "${MIGRATION_DATABASE_URL:-}" ]; then
    [ -n "${DATABASE_SECRET_ARN:-}" ] || { echo "DATABASE_SECRET_ARN is required (or MIGRATE=no)" >&2; exit 1; }
    # The URL is read into a variable and passed by environment only; it is never echoed.
    MIGRATION_DATABASE_URL="$(aws secretsmanager get-secret-value --secret-id "$DATABASE_SECRET_ARN" \
      --query SecretString --output text | python3 -c 'import sys,json; print(json.load(sys.stdin)["url"])')"
    [ -n "$MIGRATION_DATABASE_URL" ] || { echo "the secret has no \"url\" field" >&2; exit 1; }
  fi
  export MIGRATION_DATABASE_URL
  mkdir -p build
  go build -trimpath -o build/griffin-migrate ./cmd/griffin-migrate
  echo "-- before"; build/griffin-migrate status
  if [ "$MODE" = apply ]; then
    build/griffin-migrate up
    echo "-- after"; build/griffin-migrate status
  else
    echo "(plan) would run: griffin-migrate up"
  fi
  unset MIGRATION_DATABASE_URL
fi

if [ "$TARGET" != none ]; then
  echo; echo "== backend ($TARGET)"
  # shellcheck disable=SC2086
  infra/ops/deploy-backend.sh "$TARGET" $APPLY_FLAG
fi

echo; echo "== finished ($MODE, commit $SHA)"
if [ "$MODE" = apply ] && { [ "$TARGET" = all ] || [ "$TARGET" = lambda ]; }; then
  echo "Next: in the Lambda console set HOSTS_ENABLED=true when you want devices switched on."
  echo "      The web app deploys from master on Vercel by itself."
fi
