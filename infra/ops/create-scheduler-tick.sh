#!/usr/bin/env bash
# EventBridge rate(1 minute) -> API destination -> POST $API_BASE_URL/api/v1/scheduler/tick
# Idempotent (create-or-update). DRY-RUN by default: prints the exact aws commands.
# Mutates AWS only with --apply. The worker key is NEVER printed (dry-run shows <redacted>).
#
# Usage:  API_BASE_URL=https://api.example.com ./create-scheduler-tick.sh            # dry-run
#         API_BASE_URL=... WORKER_API_KEY=... ./create-scheduler-tick.sh --apply
#
# IAM needed by the caller (--apply):
#   events:CreateConnection/UpdateConnection/DescribeConnection, events:CreateApiDestination/
#   UpdateApiDestination/DescribeApiDestination, events:PutRule/PutTargets/DescribeRule,
#   iam:CreateServiceLinkedRole on apidestinations.events.amazonaws.com (one-time, first API dest),
#   iam:CreateRole/PutRolePolicy/GetRole/PassRole for griffin-scheduler-events,
#   secretsmanager:CreateSecret/PutSecretValue/DeleteSecret (EventBridge stores the key in a managed secret).
# Cost: 1 rule + 1 API destination; ~43k invocations/month (inside the free tier). The tick is a
# no-op (one DB count) when the queue is empty, so the runner still scales to zero.
set -euo pipefail
APPLY=0; [ "${1:-}" = "--apply" ] && APPLY=1
: "${API_BASE_URL:?set API_BASE_URL (https origin, no trailing slash)}"
case "$API_BASE_URL" in https://*) ;; *) echo "API_BASE_URL must be https" >&2; exit 2;; esac
R="${AWS_REGION:-ap-south-1}"; ACCT="${AWS_ACCOUNT_ID:-123456789012}"
CONN=griffin-scheduler-conn; DEST=griffin-scheduler-tick; RULE=griffin-scheduler-tick; ROLE=griffin-scheduler-events
URL="${API_BASE_URL%/}/api/v1/scheduler/tick"
if [ "$APPLY" = 1 ]; then : "${WORKER_API_KEY:?set WORKER_API_KEY for --apply}"; KEY="$WORKER_API_KEY"; else KEY="<redacted>"; fi

# run <aws args...>: prints (key masked) and executes only with --apply.
run() { printf '%s:' "$([ "$APPLY" = 1 ] && echo APPLY || echo DRY-RUN)"; printf ' %q' "${@//"$KEY"/<redacted>}"; echo; if [ "$APPLY" = 1 ]; then "$@"; fi; return 0; }
# exists <aws args...>: true only when applying and the read-only describe succeeds.
exists() { [ "$APPLY" = 1 ] && aws "$@" >/dev/null 2>&1; }

echo "# 0. one-time service-linked role (an 'already exists' error is ignored)"
if [ "$APPLY" = 1 ]; then
  aws iam create-service-linked-role --aws-service-name apidestinations.events.amazonaws.com >/dev/null 2>&1 || true
else
  echo "DRY-RUN: aws iam create-service-linked-role --aws-service-name apidestinations.events.amazonaws.com"
fi

AUTH="ApiKeyAuthParameters={ApiKeyName=X-Worker-Key,ApiKeyValue=$KEY}"
if exists events describe-connection --region "$R" --name "$CONN"; then
  run aws events update-connection --region "$R" --name "$CONN" --authorization-type API_KEY --auth-parameters "$AUTH"
else
  run aws events create-connection --region "$R" --name "$CONN" --authorization-type API_KEY --auth-parameters "$AUTH"
fi
CONN_ARN="arn:aws:events:$R:$ACCT:connection/$CONN/<id>"
if [ "$APPLY" = 1 ]; then CONN_ARN=$(aws events describe-connection --region "$R" --name "$CONN" --query ConnectionArn --output text); fi
if exists events describe-api-destination --region "$R" --name "$DEST"; then
  run aws events update-api-destination --region "$R" --name "$DEST" --connection-arn "$CONN_ARN" --invocation-endpoint "$URL" --http-method POST --invocation-rate-limit-per-second 1
else
  run aws events create-api-destination --region "$R" --name "$DEST" --connection-arn "$CONN_ARN" --invocation-endpoint "$URL" --http-method POST --invocation-rate-limit-per-second 1
fi
DEST_ARN="arn:aws:events:$R:$ACCT:api-destination/$DEST/<id>"
if [ "$APPLY" = 1 ]; then DEST_ARN=$(aws events describe-api-destination --region "$R" --name "$DEST" --query ApiDestinationArn --output text); fi

TRUST='{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"events.amazonaws.com"},"Action":"sts:AssumeRole"}]}'
POL=$(printf '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"events:InvokeApiDestination","Resource":"%s"}]}' "$DEST_ARN")
exists iam get-role --role-name "$ROLE" || run aws iam create-role --role-name "$ROLE" --assume-role-policy-document "$TRUST"
run aws iam put-role-policy --role-name "$ROLE" --policy-name invoke-tick --policy-document "$POL"
run aws events put-rule --region "$R" --name "$RULE" --schedule-expression 'rate(1 minute)' --state ENABLED
TARGETS=$(printf '[{"Id":"tick","Arn":"%s","RoleArn":"arn:aws:iam::%s:role/%s","RetryPolicy":{"MaximumRetryAttempts":0,"MaximumEventAgeInSeconds":60}}]' "$DEST_ARN" "$ACCT" "$ROLE")
run aws events put-targets --region "$R" --rule "$RULE" --targets "$TARGETS"
if [ "$APPLY" != 1 ]; then echo "# dry-run only. Re-run with --apply (and WORKER_API_KEY) to create. Disable: aws events disable-rule --name $RULE"; fi
