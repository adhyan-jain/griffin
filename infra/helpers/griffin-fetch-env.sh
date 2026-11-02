#!/usr/bin/env bash
# Writes /etc/griffin/worker.env from SSM Parameter Store (SecureString, decrypted with the
# instance role). Runs as root before each start of griffin-worker.service. Never prints values.
set -euo pipefail
umask 027

REGION="${GRIFFIN_AWS_REGION:-ap-south-1}"
PARAM_PATH="${GRIFFIN_SSM_PATH:-/griffin/prod/worker}"
OUT=/etc/griffin/worker.env
TMP="$(mktemp /etc/griffin/.worker.env.XXXXXX)"
trap 'rm -f "$TMP"' EXIT

{
  aws ssm get-parameters-by-path --region "$REGION" --path "$PARAM_PATH" --with-decryption \
    --query 'Parameters[].[Name,Value]' --output text |
    awk -F'\t' 'NF==2 { n=$1; sub(".*/", "", n); print n "=" $2 }'
  cat <<CFG
ENVIRONMENT=production
GRIFFIN_MODE=worker
AI_PROVIDER=antigravity
ANTIGRAVITY_BINARY=/home/griffin/.local/bin/agy
WORKSPACE_ROOT=/var/griffin/workspaces
SCALE_TO_ZERO_ENABLED=true
IDLE_SHUTDOWN_SECONDS=300
STORAGE_PROVIDER=s3
S3_REGION=${REGION}
AWS_REGION=${REGION}
MAX_CONCURRENT_WORKERS=2
MAX_CONCURRENT_ANTIGRAVITY=1
CFG
} > "$TMP"

# Fail closed: the required secrets must be present before replacing the live file.
for key in DATABASE_URL SUPABASE_JWT_SECRET WORKER_API_KEY S3_BUCKET; do
  grep -q "^${key}=." "$TMP" || { echo "missing required parameter ${key} under ${PARAM_PATH}" >&2; exit 1; }
done

chown root:griffin "$TMP"
chmod 0640 "$TMP"
mv -f "$TMP" "$OUT"
trap - EXIT
