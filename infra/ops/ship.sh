#!/usr/bin/env bash
# Ships what is committed on master and proves it landed. Dry run unless --apply is given.
#
#   infra/ops/ship.sh            show what would happen
#   infra/ops/ship.sh --apply    do it
#
# Steps (each prints what it does, and the script stops at the first failure):
#   1. refuse a dirty tree or a branch other than master
#   2. restart the idle worker, so it re-checks unfinished plans on startup and finishes them
#   3. deploy the API Lambda (infra/ops/deploy-backend.sh lambda --apply, which smoke-tests it)
#   4. git push origin master (Vercel then deploys the web app)
#   5. verify live: the new API routes exist, Vercel reports success, the new web code is served,
#      and the worker logged that it finished plans
# It never creates infrastructure, changes IAM, launches instances or replaces the worker binary
# (that is a separate, deliberate step: infra/ops/deploy-backend.sh worker).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

APPLY=0
[ "${1:-}" = "--apply" ] && APPLY=1
export AWS_PROFILE="${AWS_PROFILE:-griffin-bootstrap}" AWS_REGION="${AWS_REGION:-ap-south-1}"
INSTANCE="${INSTANCE_ID:-i-0123456789abcdef0}"
API="${API_URL:-https://fq322jvjt4.execute-api.ap-south-1.amazonaws.com}"
WEB="${WEB_URL:-https://griffin-pwa.vercel.app}"

say() { printf '\n== %s\n' "$*"; }
run() { if [ "$APPLY" = 1 ]; then printf '+ %s\n' "$*"; "$@"; else printf '(dry run) %s\n' "$*"; fi; }

say "1. checks"
[ "$(git branch --show-current)" = "master" ] || { echo "not on master"; exit 1; }
[ -z "$(git status --porcelain --untracked-files=no)" ] || { echo "uncommitted changes: commit or stash first"; exit 1; }
AHEAD="$(git rev-list --count origin/master..master 2>/dev/null || echo "?")"
echo "commits to ship: $AHEAD ($(git rev-parse --short HEAD))"
[ "$APPLY" = 1 ] || echo "DRY RUN: nothing below is executed. Add --apply to ship."

say "2. restart the worker (it is idle; on start it finishes plans whose subtasks are all done)"
run aws ssm send-command --instance-ids "$INSTANCE" --document-name AWS-RunShellScript \
  --parameters 'commands=["systemctl restart griffin-worker"]' --query Command.CommandId --output text

say "3. deploy the API Lambda"
run infra/ops/deploy-backend.sh lambda --apply

say "4. push master (Vercel deploys the web app)"
run git push origin master

[ "$APPLY" = 1 ] || exit 0

say "5. verify"
FAIL=0
check() { if "$@"; then echo "ok    $DESC"; else echo "FAIL  $DESC"; FAIL=1; fi; }

DESC="API has the Lists route (401 without a token, not 404)"
check test "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/api/v1/lists")" = 401

SHA="$(git rev-parse HEAD)"
echo "waiting for Vercel to build $SHA ..."
STATE=pending
for _ in $(seq 1 45); do
  STATE="$(gh api "repos/$(git config --get remote.origin.url | sed -E 's#(git@github.com:|https://github.com/)##; s#\.git$##')/commits/$SHA/status" --jq .state 2>/dev/null || echo pending)"
  [ "$STATE" = success ] || [ "$STATE" = failure ] || [ "$STATE" = error ] && break
  sleep 20
done
DESC="Vercel build finished with success (was: $STATE)"
check test "$STATE" = success

DESC="live web serves the new code (Save to list, in the task and list routes)"
found=0
# "Save to list" lives in the task-detail and Lists chunks, which the home page does not load.
for p in /lists /tasks/00000000-0000-0000-0000-000000000000; do
  for c in $(curl -s "$WEB$p" | grep -o '/_next/static/[^"\\]*\.js' | sort -u); do
    curl -s "$WEB$c" | grep -q 'Save to list' && found=1 && break 2
  done
done
check test "$found" = 1

echo "waiting for the worker to finish stuck plans ..."
sleep 20
CID="$(aws ssm send-command --instance-ids "$INSTANCE" --document-name AWS-RunShellScript \
  --parameters 'commands=["journalctl -u griffin-worker --no-pager --since \"-3min\" | grep -cE \"JJ completed orchestration|Reconciling active\""]' \
  --query Command.CommandId --output text)"
sleep 6
N="$(aws ssm get-command-invocation --command-id "$CID" --instance-id "$INSTANCE" --query StandardOutputContent --output text | tr -d '[:space:]')"
DESC="worker restarted and reconciled plans (log lines: ${N:-0})"
check test "${N:-0}" -ge 1

[ "$FAIL" = 0 ] && say "all checks passed" || { say "some checks failed (see above)"; exit 1; }
