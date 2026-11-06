#!/usr/bin/env bash
# Deploys the Griffin backend: the API Lambda, the EC2 worker binary, or both.
# DRY RUN by default: prints every command and runs nothing. Add --apply to execute.
#
# Usage:  infra/ops/deploy-backend.sh lambda|worker|all [--apply]
#         infra/ops/deploy-backend.sh rollback-worker [--apply]
#
# Needs credentials that may update the Lambda (lambda:GetFunctionConfiguration, UpdateFunctionCode,
# PublishVersion; see docs/FINAL_IAM_POLICIES/D-deployer-api-scheduler-ops.json) and use SSM/EC2/S3 for
# the worker. Nothing here creates infrastructure, launches instances or changes IAM.
#
# Env (defaults match production): AWS_PROFILE, AWS_REGION=ap-south-1, LAMBDA_FUNCTION=griffin-api,
#   INSTANCE_ID=i-0123456789abcdef0, BUCKET=griffin-artifacts-123456789012-ap-south-1,
#   API_URL=https://fq322jvjt4.execute-api.ap-south-1.amazonaws.com
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

TARGET="${1:-}"; APPLY=0; [ "${2:-}" = "--apply" ] && APPLY=1
export AWS_REGION="${AWS_REGION:-ap-south-1}" AWS_DEFAULT_REGION="${AWS_REGION:-ap-south-1}"
FN="${LAMBDA_FUNCTION:-griffin-api}"
INSTANCE_ID="${INSTANCE_ID:-i-0123456789abcdef0}"
BUCKET="${BUCKET:-griffin-artifacts-123456789012-ap-south-1}"
API_URL="${API_URL:-https://fq322jvjt4.execute-api.ap-south-1.amazonaws.com}"
SHA="$(git rev-parse --short HEAD)"

case "$TARGET" in lambda|worker|all|rollback-worker) ;; *) sed -n '2,12p' "$0"; exit 2;; esac
[ -z "$(git status --porcelain --untracked-files=no)" ] || { echo "refusing to deploy with uncommitted changes (build must match commit $SHA)" >&2; exit 1; }

run() { echo "+ $*"; [ "$APPLY" -eq 1 ] && "$@" || true; }
say() { echo; echo "== $*"; }
[ "$APPLY" -eq 1 ] && echo "APPLY mode: deploying commit $SHA" || echo "DRY RUN (add --apply to execute). Commit $SHA"

smoke_api() {
  say "smoke test: $API_URL"
  for spec in "GET /healthz 200" "GET /api/v1/system/status 401" "POST /api/v1/jj/messages 401"; do
    set -- $spec
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -X "$1" "$API_URL$2" -H 'Origin: https://griffin-pwa.vercel.app')"
    [ "$code" = "$3" ] && echo "ok    $1 $2 -> $code" || { echo "FAIL  $1 $2 -> $code (wanted $3)"; return 1; }
  done
}

deploy_lambda() {
  say "API Lambda ($FN)"
  run bash scripts/package-lambda.sh build/griffin-api.zip
  if [ "$APPLY" -eq 1 ]; then
    PREV="$(aws lambda get-function-configuration --function-name "$FN" --query CodeSha256 --output text)"
    echo "previous CodeSha256: $PREV (rebuild that commit with scripts/package-lambda.sh to roll back)"
  fi
  # --query keeps the function's environment (database URLs, API keys) out of the output and any log of it.
  run aws lambda update-function-code --function-name "$FN" --zip-file fileb://build/griffin-api.zip --architectures arm64 --publish \
    --query '{Version:Version,CodeSha256:CodeSha256,LastModified:LastModified,State:State}' --output json
  run aws lambda wait function-updated --function-name "$FN"
  [ "$APPLY" -eq 1 ] && smoke_api || echo "(after --apply: smoke test of /healthz, /api/v1/system/status and /api/v1/jj/messages)"
}

remote_script() { cat <<'EOS'
set -u
REL_URL="$1"; WANT_SHA="$2"
cd /tmp && rm -rf gw && mkdir gw && cd gw
aws s3 cp "$REL_URL" ./gw.tgz --only-show-errors || { echo "download failed"; exit 1; }
echo "$WANT_SHA  gw.tgz" | sha256sum -c - || { echo "checksum mismatch"; exit 1; }
tar -xzf gw.tgz ./griffin-server
file griffin-server | grep -q "ARM aarch64" || { echo "not an arm64 binary"; exit 1; }
systemctl stop griffin-worker
cp -p /opt/griffin/griffin-server /opt/griffin/griffin-server.prev
install -o griffin -g griffin -m 755 griffin-server /opt/griffin/griffin-server
systemctl start griffin-worker
sleep 15
if systemctl is-active --quiet griffin-worker && ! journalctl -u griffin-worker --since "-20 sec" --no-pager | grep -qiE "panic|fatal"; then
  echo "WORKER_OK $(systemctl show griffin-worker -p ActiveEnterTimestamp --value)"
else
  echo "WORKER_UNHEALTHY: restoring previous binary"
  systemctl stop griffin-worker; install -o griffin -g griffin -m 755 /opt/griffin/griffin-server.prev /opt/griffin/griffin-server; systemctl start griffin-worker
  exit 1
fi
EOS
}

ensure_runner_up() {
  for n in 1 2 3 4 5 6 7 8; do
    state="$(aws ec2 describe-instances --instance-ids "$INSTANCE_ID" --query 'Reservations[0].Instances[0].State.Name' --output text)"
    [ "$state" = running ] && break
    out="$(aws ec2 start-instances --instance-ids "$INSTANCE_ID" 2>&1 || true)"
    echo "start attempt $n: $(echo "$out" | grep -o 'InsufficientInstanceCapacity' || echo started)"
    echo "$out" | grep -q InsufficientInstanceCapacity && sleep 30 || break
  done
  for _ in $(seq 1 40); do
    [ "$(aws ssm describe-instance-information --filters Key=InstanceIds,Values="$INSTANCE_ID" --query 'InstanceInformationList[0].PingStatus' --output text 2>/dev/null)" = Online ] && return 0
    sleep 5
  done
  echo "runner did not come online (AWS capacity?). Retry later." >&2; return 1
}

ssm_run() { # $1 = base64 script, $2.. = args
  B64="$1"; shift
  CID="$(aws ssm send-command --instance-ids "$INSTANCE_ID" --document-name AWS-RunShellScript --comment "griffin deploy $SHA" \
    --parameters "commands=[\"echo $B64 | base64 -d | bash -s -- $*\"]" --query Command.CommandId --output text)"
  for _ in $(seq 1 60); do
    s="$(aws ssm get-command-invocation --command-id "$CID" --instance-id "$INSTANCE_ID" --query Status --output text 2>/dev/null || true)"
    [ "$s" = Success ] || [ "$s" = Failed ] && break; sleep 3
  done
  aws ssm get-command-invocation --command-id "$CID" --instance-id "$INSTANCE_ID" --query StandardOutputContent --output text
  [ "$s" = Success ]
}

deploy_worker() {
  say "EC2 worker ($INSTANCE_ID)"
  run bash scripts/package-worker.sh build/griffin-worker.tar.gz
  if [ "$APPLY" -eq 1 ]; then
    WSHA="$(sha256sum build/griffin-worker.tar.gz | cut -d' ' -f1)"
    aws s3 cp build/griffin-worker.tar.gz "s3://$BUCKET/releases/$SHA/griffin-worker.tar.gz" --only-show-errors
    ensure_runner_up
    ssm_run "$(remote_script | base64 -w0)" "s3://$BUCKET/releases/$SHA/griffin-worker.tar.gz" "$WSHA"
  else
    echo "+ aws s3 cp build/griffin-worker.tar.gz s3://$BUCKET/releases/$SHA/griffin-worker.tar.gz"
    echo "+ (start the runner if stopped, retrying on InsufficientInstanceCapacity)"
    echo "+ (over SSM: verify sha256, stop service, back up /opt/griffin/griffin-server.prev, install, start, health check, auto-restore on failure)"
  fi
}

rollback_worker() {
  say "restore the previous worker binary (/opt/griffin/griffin-server.prev)"
  if [ "$APPLY" -eq 1 ]; then
    ensure_runner_up
    ssm_run "$(echo 'set -e; test -f /opt/griffin/griffin-server.prev; systemctl stop griffin-worker; install -o griffin -g griffin -m 755 /opt/griffin/griffin-server.prev /opt/griffin/griffin-server; systemctl start griffin-worker; sleep 5; systemctl is-active griffin-worker' | base64 -w0)"
  else
    echo "+ (over SSM: stop service, restore .prev, start, verify active)"
  fi
}

case "$TARGET" in
  lambda) deploy_lambda ;;
  worker) deploy_worker ;;
  all) deploy_lambda; deploy_worker ;;
  rollback-worker) rollback_worker ;;
esac
say "done"
