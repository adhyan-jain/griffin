# Final IAM policies (Griffin, account 220115213279, ap-south-1)

Status: **REQUIRES_USER_APPROVAL** — none attached. JSON is syntactically valid. AWS's own
`access-analyzer validate-policy` could not be run (`griffin-bootstrap` is denied), so an admin must run it
before attaching (`aws accessanalyzer validate-policy --policy-type IDENTITY_POLICY --policy-document file://X.json`).
`griffin-bootstrap` cannot self-grant; an admin identity attaches everything below.

> Naming note: `DEPLOYMENT_PREFLIGHT_REPORT.md` calls the API/scheduler deployer policy "Policy B" and
> `FINAL_DEPLOYMENT_READINESS.md` calls the Lambda execution role "Policy B". **These files supersede both**:
> A = deployer (EC2/SSM), B = Lambda runtime, C = worker runtime, D = deployer (API/scheduler/ops), E = EventBridge invoke role.

| File | Attach to | Purpose |
|---|---|---|
| `A-deployer-ec2-ssm.json` | user `griffin-bootstrap` (inline `GriffinDeployerEc2Ssm`) | launch the one runner, Session Manager / Run Command, start/stop/reboot tagged runner for acceptance tests, upload release bundle to `releases/*` |
| `D-deployer-api-scheduler-ops.json` | user `griffin-bootstrap` (inline `GriffinDeployerApiOps`) — only at checkpoint 4 | create `griffin-api` Lambda, HTTP API, EventBridge rule/connection/destination, two roles, SSM secrets, S3 lifecycle, budgets, alarms |
| `B-lambda-execution-role.json` + `B-lambda-trust.json` | new role `griffin-api-lambda-role` (+ `AWSLambdaBasicExecutionRole`) | runtime: wake the runner, presign/read/write `users/*` artifacts |
| `C-worker-role-runtime.json` | **add** as new inline policy `GriffinWorkerRuntime` on `GriffinEC2WorkerRole` | read `/griffin/prod/worker*` params, artifact RW (incl. `DeleteObject`: the upload path deletes the object if persisting its DB row fails; the bucket is versioned so this only adds a delete marker). Existing `GriffinArtifactStorageAccess` and `AmazonSSMManagedInstanceCore` stay; do not recreate the role |
| `E-events-invoke-role.json` + `E-events-trust.json` | new role `griffin-events-invoke-role` | EventBridge → API destination `griffin-tick` only |

## Operations required vs. removed
- **Removed from earlier drafts:** `ec2:TerminateInstances`; every `iam:Create*` for the worker role/profile (they exist; the
  provision script no longer touches IAM); `iam:PutRolePolicy` on the worker role (admin attaches C — otherwise the deployer
  could rewrite the worker's own permissions); `ec2:StopInstances` and `ec2:DescribeInstances`-wide use on Lambda beyond what is
  listed; ACM/custom-domain actions (initial deployment uses the provider-assigned `execute-api` URL).
- **Pass-role:** only `GriffinEC2WorkerRole` → `ec2.amazonaws.com` (A); only the two new roles → lambda/events (D).
- **EC2 restrictions (A):** `t4g.medium` only; IMDSv2 required; `Project=Griffin` tag required on create; volume must be encrypted and ≤ 30 GiB.
- **Lambda scope (B):** `ec2:StartInstances` on the single instance ARN (fill `<INSTANCE_ID>` after launch) with `Project=Griffin` tag; S3 limited to `users/*`.
  `StopInstances` is deliberately absent — see open decision "runner/sleep" in `CONTROLLED_DEPLOYMENT_REPORT.md`.

## Cannot be scoped to individual resources (AWS limitation)
`ec2:Describe*`; `ssm:DescribeInstanceInformation/DescribeSessions/GetConnectionStatus/GetCommandInvocation/ListCommandInvocations`;
`ssm:DescribeParameters`; `ce:GetCostAndUsage`; `cloudwatch:GetMetric*`; `apigateway:*` can only be limited to the `/apis` path
family, not to one API before it exists; `ec2:CreateSecurityGroup` on the `security-group/*` ARN (id unknown beforehand).

## NOT VERIFIED
The Secrets Manager statement for the EventBridge connection secret (`events!connection/griffin-*`) follows AWS documentation but
has not been exercised; if `create-connection` is denied, the denial message names the missing action. The `ssm:resourceTag`
condition on the `SSM-SessionManagerRunShell` document ARN may need splitting if `start-session` is denied.
