#!/usr/bin/env bash
# Creates $10 and $20 monthly cost budgets with email alerts. PRINTS the commands by default;
# set GRIFFIN_APPLY=1 to execute (requires Policy D 'Budgets' and approval checkpoint 5).
set -euo pipefail
: "${ALERT_EMAIL:?set ALERT_EMAIL}"; ACCT="${AWS_ACCOUNT_ID:-220115213279}"
run() { if [ "${GRIFFIN_APPLY:-0}" = "1" ]; then "$@"; else printf 'DRY-RUN:'; printf ' %q' "$@"; echo; fi; }
for amt in 10 20; do
  budget=$(printf '{"BudgetName":"griffin-monthly-%s","BudgetLimit":{"Amount":"%s","Unit":"USD"},"TimeUnit":"MONTHLY","BudgetType":"COST"}' "$amt" "$amt")
  notif=$(printf '[{"Notification":{"NotificationType":"ACTUAL","ComparisonOperator":"GREATER_THAN","Threshold":80,"ThresholdType":"PERCENTAGE"},"Subscribers":[{"SubscriptionType":"EMAIL","Address":"%s"}]},{"Notification":{"NotificationType":"FORECASTED","ComparisonOperator":"GREATER_THAN","Threshold":100,"ThresholdType":"PERCENTAGE"},"Subscribers":[{"SubscriptionType":"EMAIL","Address":"%s"}]}]' "$ALERT_EMAIL" "$ALERT_EMAIL")
  run aws budgets create-budget --account-id "$ACCT" --budget "$budget" --notifications-with-subscribers "$notif"
done
