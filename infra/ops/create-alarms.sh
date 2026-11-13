#!/usr/bin/env bash
# SNS topic + CloudWatch alarms for failures. PRINTS by default; GRIFFIN_APPLY=1 executes
# (requires Policy D 'AlertsTopicAndAlarms', and the Lambda/API/instance to exist).
# Needs: ALERT_EMAIL, API_ID (HTTP API id), INSTANCE_ID.
set -euo pipefail
: "${ALERT_EMAIL:?}" "${API_ID:?}" "${INSTANCE_ID:?}"; R=ap-south-1; ACCT="${AWS_ACCOUNT_ID:?set AWS_ACCOUNT_ID}"
TOPIC="arn:aws:sns:$R:$ACCT:griffin-alerts"
run() { if [ "${GRIFFIN_APPLY:-0}" = "1" ]; then "$@"; else printf 'DRY-RUN:'; printf ' %q' "$@"; echo; fi; }
run aws sns create-topic --region $R --name griffin-alerts
run aws sns subscribe --region $R --topic-arn "$TOPIC" --protocol email --notification-endpoint "$ALERT_EMAIL"
alarm() { run aws cloudwatch put-metric-alarm --region $R --alarm-name "$1" --namespace "$2" --metric-name "$3" \
  --dimensions "Name=$4,Value=$5" --statistic Sum --period 300 --evaluation-periods 1 --threshold "$6" \
  --comparison-operator GreaterThanOrEqualToThreshold --treat-missing-data notBreaching --alarm-actions "$TOPIC"; }
alarm griffin-lambda-errors AWS/Lambda Errors FunctionName griffin-api 1
alarm griffin-api-5xx AWS/ApiGateway 5xx ApiId "$API_ID" 5
alarm griffin-runner-status-check AWS/EC2 StatusCheckFailed InstanceId "$INSTANCE_ID" 1
