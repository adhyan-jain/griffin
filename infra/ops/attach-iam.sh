#!/usr/bin/env bash
# Validates (AWS Access Analyzer) and attaches the Griffin IAM policies A, C and D.
# MUST be run with an ADMIN profile that can write IAM (NOT griffin-bootstrap, which cannot self-grant):
#   ADMIN_PROFILE=my-admin ./infra/ops/attach-iam.sh            # validate only (default, changes nothing)
#   ADMIN_PROFILE=my-admin ./infra/ops/attach-iam.sh --apply    # validate, then attach if there are no ERROR findings
# Your account (AWS_ACCOUNT_ID), region ap-south-1. Idempotent: put-*-policy replaces the same-named inline policy.
set -euo pipefail
: "${ADMIN_PROFILE:?set ADMIN_PROFILE to an admin AWS CLI profile}"
: "${AWS_ACCOUNT_ID:?set AWS_ACCOUNT_ID to the 12-digit account these policies are for}"
APPLY=0; [ "${1:-}" = "--apply" ] && APPLY=1
cd "$(dirname "${BASH_SOURCE[0]}")/../../docs/FINAL_IAM_POLICIES"
export AWS_PAGER=""
P=(--profile "$ADMIN_PROFILE" --region ap-south-1)

ACCT=$(aws sts get-caller-identity "${P[@]}" --query Account --output text)
[ "$ACCT" = "$AWS_ACCOUNT_ID" ] || { echo "Refusing: profile is account $ACCT, expected $AWS_ACCOUNT_ID" >&2; exit 1; }
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

# A and D exceed the 2048-char inline limit for IAM *users*, so they are standalone managed policies (limit 6144 each).
attach_managed() { # name file
  local name=$1 file=$2 arn="arn:aws:iam::$AWS_ACCOUNT_ID:policy/$1"
  if aws iam get-policy "${P[@]}" --policy-arn "$arn" >/dev/null 2>&1; then
    aws iam create-policy-version "${P[@]}" --policy-arn "$arn" --policy-document "file://$file" --set-as-default >/dev/null
    echo "updated $name (new default version)"
  else
    aws iam create-policy "${P[@]}" --policy-name "$name" --policy-document "file://$file" >/dev/null
    echo "created $name"
  fi
  aws iam attach-user-policy "${P[@]}" --user-name griffin-bootstrap --policy-arn "$arn"
}
attach_managed GriffinDeployerEc2Ssm A-deployer-ec2-ssm.json
attach_managed GriffinDeployerApiOps D-deployer-api-scheduler-ops.json
# C is small (680 chars): inline on the worker role, alongside its existing policies.
aws iam put-role-policy "${P[@]}" --role-name GriffinEC2WorkerRole --policy-name GriffinWorkerRuntime --policy-document file://C-worker-role-runtime.json
echo "Attached: managed GriffinDeployerEc2Ssm + GriffinDeployerApiOps on user griffin-bootstrap; inline GriffinWorkerRuntime on role GriffinEC2WorkerRole."
