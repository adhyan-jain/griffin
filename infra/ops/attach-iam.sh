#!/usr/bin/env bash
# Validates (AWS Access Analyzer) and attaches the Griffin IAM policies A, C and D.
# MUST be run with an ADMIN profile that can write IAM (NOT griffin-bootstrap, which cannot self-grant):
#   ADMIN_PROFILE=my-admin ./infra/ops/attach-iam.sh            # validate only (default, changes nothing)
#   ADMIN_PROFILE=my-admin ./infra/ops/attach-iam.sh --apply    # validate, then attach if there are no ERROR findings
# Account 123456789012, region ap-south-1. Idempotent: put-*-policy replaces the same-named inline policy.
set -euo pipefail
: "${ADMIN_PROFILE:?set ADMIN_PROFILE to an admin AWS CLI profile}"
APPLY=0; [ "${1:-}" = "--apply" ] && APPLY=1
cd "$(dirname "${BASH_SOURCE[0]}")/../../docs/FINAL_IAM_POLICIES"
export AWS_PAGER=""
P=(--profile "$ADMIN_PROFILE" --region ap-south-1)

ACCT=$(aws sts get-caller-identity "${P[@]}" --query Account --output text)
[ "$ACCT" = "123456789012" ] || { echo "Refusing: profile is account $ACCT, expected 123456789012" >&2; exit 1; }
echo "Using $(aws sts get-caller-identity "${P[@]}" --query Arn --output text)"

fail=0
for f in A-deployer-ec2-ssm C-worker-role-runtime D-deployer-api-scheduler-ops; do
  echo "== validate $f"
  errs=$(aws accessanalyzer validate-policy "${P[@]}" --policy-type IDENTITY_POLICY \
        --policy-document "file://$f.json" --query 'length(findings[?findingType==`ERROR`])' --output text)
  aws accessanalyzer validate-policy "${P[@]}" --policy-type IDENTITY_POLICY --policy-document "file://$f.json" \
        --query 'findings[].[findingType,issueCode,locations[0].span.start.line]' --output text || true
  [ "$errs" = "0" ] || { echo "  $errs ERROR finding(s) in $f" >&2; fail=1; }
done
[ "$fail" = "0" ] || { echo "Fix ERROR findings first; nothing attached." >&2; exit 1; }

if [ "$APPLY" != "1" ]; then echo "Validation passed. Re-run with --apply to attach."; exit 0; fi

aws iam put-user-policy "${P[@]}" --user-name griffin-bootstrap --policy-name GriffinDeployerEc2Ssm   --policy-document file://A-deployer-ec2-ssm.json
aws iam put-user-policy "${P[@]}" --user-name griffin-bootstrap --policy-name GriffinDeployerApiOps   --policy-document file://D-deployer-api-scheduler-ops.json
aws iam put-role-policy "${P[@]}" --role-name GriffinEC2WorkerRole --policy-name GriffinWorkerRuntime  --policy-document file://C-worker-role-runtime.json
echo "Attached: GriffinDeployerEc2Ssm, GriffinDeployerApiOps (user griffin-bootstrap); GriffinWorkerRuntime (role GriffinEC2WorkerRole)."
