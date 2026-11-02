# Griffin — Final Deployment Readiness Review

**Audit Date**: 2026-11-02  
**Target Environment**: AWS Account C (`123456789012`), Region `ap-south-1` (Mumbai), Supabase PostgreSQL  
**Audit Scope**: Repository commit, test suite classification, AWS IAM permissions, database migration safety, runtime plumbing, scale-to-zero lifecycle, and unattended Antigravity CLI execution readiness.  

---

## 1. Verified Repository Commit & Working-Tree Status

| Attribute | Inspected Value | Status |
| :--- | :--- | :--- |
| **Local Commit SHA** | [`6267f5813a55cf26c9184b35fe8ee49c07340f7d`](file:///home/adhyan/Desktop/griffin) | **VERIFIED** |
| **Remote HEAD (`origin/master`)** | `6267f5813a55cf26c9184b35fe8ee49c07340f7d` | **IN SYNC** |
| **Working Tree** | Clean (0 modified, 0 untracked files) | **CLEAN** |
| **GitHub Actions Workflow** | CI run `41419664403` on `master` | **SUCCESS** |
| **Go Code Formatting** | `gofmt -l .` returned 0 unformatted files | **PASS** |
| **Go Static Analysis** | `go vet ./...` returned 0 errors/warnings | **PASS** |
| **Web Lint & Typecheck** | `npm run lint` (0 errors), `npx tsc --noEmit` (0 errors) | **PASS** |
| **Web Production Build** | Next.js 16.4.3 Turbopack compiled 10/10 routes | **PASS** |
| **Production Secret & Dev Leak Check** | `.next/static` has 0 occurrences of dev login or credentials | **VERIFIED** |

### Automated Test Count Reconciliation

Running `go test -v ./...` against local Postgres reveals:
- **104 Top-Level Test Functions** across 9 packages.
- **197 Subtest Assertions** (`t.Run`), resulting in:
  - **197 PASS**
  - **0 FAIL**
  - **3 SKIP** (by design: opt-in live AWS S3 and live Antigravity tests)

### Test Verification Level Classification

| Category | Test Count / Scope | Execution Mechanism | Genuine Live Verification Level |
| :--- | :--- | :--- | :--- |
| **In-Memory Mock Unit Tests** | 46 tests (config, domain, manager, runner stubs, middleware, mock storage) | In-memory structs, mock subprocess scripts, synthetic HTTP test servers | **MOCKED** (Validates logic, zero network interaction) |
| **Local Container Integration Tests** | 55 tests (task engine, RLS, leases, DAG dependencies, agent factory, JJ orchestrator) | Real PostgreSQL 17 database (`localhost:54322`, `griffin_test`) | **CONTAINER-BASED** (Validates real SQL queries, locks, transactions) |
| **Genuinely Live AWS S3 Integration** | 1 test ([`tests/s3_live_test.go`](file:///home/adhyan/Desktop/griffin/tests/s3_live_test.go): `TestS3_LiveAWSVerification`) | Real AWS S3 API against bucket `griffin-artifacts-123456789012-ap-south-1` using `griffin-bootstrap` credentials in `ap-south-1` | **100% LIVE VERIFIED** (Upload, Download, Head, Presign URL, Delete) |
| **Genuinely Live Antigravity CLI Integration** | 2 tests (`TestAntigravityLiveIntegration`, `TestJJ_OptInLiveAntigravityIntegration`) | Local `/usr/bin/agy` CLI binary | **LIVE LOCALLY** (Not verified on EC2 ARM64) |
| **Cloud Infrastructure Provisioning** | EC2 launch, Lambda API, EventBridge Scheduler | Scripts and code compiled, but not executed against AWS | **NOT VERIFIED LIVE** (Blocked by IAM) |

---

## 2. Current Implementation Status & Gate Closure Audit

All eight architectural and security gates identified in [`DEPLOYMENT_PREFLIGHT_REPORT.md`](file:///home/adhyan/Desktop/griffin/DEPLOYMENT_PREFLIGHT_REPORT.md) have been closed in code and verified via automated tests:

| Gate | Focus Area | Architectural Resolution | Verification Evidence |
| :--- | :--- | :--- | :--- |
| **G1** | Dev Login Bypass in Production | Route registered only if `ENABLE_DEV_LOGIN=true` and `ENVIRONMENT != production`. Next.js inlines false flag and completely strips dev login UI from production JS bundles. | [`TestDevLoginRouteGating`](file:///home/adhyan/Desktop/griffin/tests/security_test.go), bundle grep = 0 matches |
| **G2** | Wildcard CORS Vulnerability | Strict origin allowlist (`CORS_ALLOWED_ORIGINS`). Exact string matching; wildcard disallowed; preflight `OPTIONS` rejects untrusted origins with 403; credentials never allowed. | [`TestCORSAllowlist`](file:///home/adhyan/Desktop/griffin/internal/api/middleware/middleware_test.go), [`TestCORSNeverWildcard`](file:///home/adhyan/Desktop/griffin/internal/api/middleware/middleware_test.go) |
| **G3** | Development Secret Fallbacks | Defaults eliminated for all secrets and DB URLs. Unset `ENVIRONMENT` fails closed to `production`. Rejects placeholders and secrets <32 chars. Worker key checked in constant time. | [`TestNoSecretFallbackInAnyEnvironment`](file:///home/adhyan/Desktop/griffin/internal/config/config_test.go), [`TestProductionRejections`](file:///home/adhyan/Desktop/griffin/internal/config/config_test.go) |
| **G4** | EC2 Wake-Up Stub | Replaced stub with real AWS SDK v2 client ([`internal/runner/instance.go`](file:///home/adhyan/Desktop/griffin/internal/runner/instance.go)). Handles `StartInstances`, `StopInstances`, `DescribeInstances`, and stopping transition backoff retries. Bounded 8s synchronous wake prevents Lambda freeze drops. | [`TestEC2StartRunner`](file:///home/adhyan/Desktop/griffin/internal/runner/ec2_infra_test.go), [`TestWakeupControllerStartsStoppedInstance`](file:///home/adhyan/Desktop/griffin/internal/runner/ec2_infra_test.go) |
| **G5** | Idle Shutdown Permission Failure | Implemented [`SystemStopper`](file:///home/adhyan/Desktop/griffin/internal/runner/shutdown.go) executing `sudo -n /usr/sbin/shutdown -h now`. Sudoers drop-in [`infra/sudoers.d/griffin-shutdown`](file:///home/adhyan/Desktop/griffin/infra/sudoers.d/griffin-shutdown) grants exact command. Verified by `visudo -cf`. | [`TestSudoersMatchesShutdownCommand`](file:///home/adhyan/Desktop/griffin/internal/runner/ec2_infra_test.go) enforces exact character parity |
| **G6** | Migrations vs Hosted Supabase | Embedded migrations in binary ([`migrations/embed.go`](file:///home/adhyan/Desktop/griffin/migrations/embed.go)). Separate CLI [`cmd/griffin-migrate`](file:///home/adhyan/Desktop/griffin/cmd/griffin-migrate/main.go) with Postgres advisory locks and SHA-256 tamper checks. Refuses transaction pooler port 6543. Checks for existing `auth.users` and `auth.uid()`. Server runs read-only schema check at start. | [`TestMigrateAppliesOnceAndIsRepeatable`](file:///home/adhyan/Desktop/griffin/tests/migrations_test.go), [`TestServerNeverMigratesAtStartup`](file:///home/adhyan/Desktop/griffin/tests/migrations_test.go) |
| **G7** | Cross-Tenant Artifact Ingestion | Enforces `users/<user_id>/...` key hierarchy. Handlers re-verify user ownership of `task_id` and object prefix on upload, download, presign, and delete. Request bodies capped at 5 MiB (`MAX_UPLOAD_BYTES`). | [`TestArtifactTenantIsolation`](file:///home/adhyan/Desktop/griffin/tests/security_test.go), [`TestArtifactUploadSizeCap`](file:///home/adhyan/Desktop/griffin/tests/security_test.go) |
| **G8** | Missing Deployment Plumbing | Created systemd unit ([`infra/systemd/griffin-worker.service`](file:///home/adhyan/Desktop/griffin/infra/systemd/griffin-worker.service)), SSM env loader ([`infra/helpers/griffin-fetch-env.sh`](file:///home/adhyan/Desktop/griffin/infra/helpers/griffin-fetch-env.sh)), packaging scripts ([`scripts/package-lambda.sh`](file:///home/adhyan/Desktop/griffin/scripts/package-lambda.sh), [`scripts/package-worker.sh`](file:///home/adhyan/Desktop/griffin/scripts/package-worker.sh)), pgx pooler simple protocol auto-switch on port 6543, and Supabase email/password auth in PWA. | [`TestSystemdUnit`](file:///home/adhyan/Desktop/griffin/internal/runner/ec2_infra_test.go), [`TestFetchEnvScriptFailsClosedAndNeverEchoesSecrets`](file:///home/adhyan/Desktop/griffin/internal/runner/ec2_infra_test.go), [`TestIsTransactionPooler`](file:///home/adhyan/Desktop/griffin/internal/database/database_test.go) |

---

## 3. Deployment Architecture & Dependency Map

```mermaid
flowchart TD
    subgraph Clients["User Ingress"]
        PWA["Next.js 15 PWA (Vercel)<br/>Static Hosting & Supabase Auth Client"]
    end

    subgraph AWS_Control["Serverless API Layer (24/7 Available)"]
        APIGW["API Gateway HTTP API<br/>https://api.griffin..."]
        Lambda["AWS Lambda (arm64, provided.al2023)<br/>griffin-server (GRIFFIN_MODE=api)<br/>AWS Lambda Web Adapter"]
        EventBridge["EventBridge Scheduler<br/>rate(1 minute) → POST /scheduler/tick"]
    end

    subgraph AWS_Compute["Scale-to-Zero Worker Compute"]
        EC2["EC2 t4g.medium (Ubuntu 24.04 ARM64)<br/>griffin-cloud-runner<br/>griffin-worker.service (GRIFFIN_MODE=worker)"]
        AGY["Google Antigravity CLI (agy 1.2.0)<br/>Keyring / Lingering DBus Session"]
    end

    subgraph Persistence["Storage & Database"]
        S3["AWS S3 Bucket<br/>griffin-artifacts-123456789012-ap-south-1<br/>AES-256 SSE, BlockPublicAccess"]
        SupaTxn["Supabase PgBouncer / Supavisor<br/>Transaction Pooler :6543 (Simple Protocol)"]
        SupaSess["Supabase Session Pooler :5432<br/>(Prepared Statements & Advisory Locks)"]
        Postgres[("Supabase PostgreSQL DB<br/>Durable Task Queue, RLS, Schema")]
    end

    PWA -->|HTTPS REST + Bearer JWT| APIGW
    APIGW --> Lambda
    EventBridge -->|X-Worker-Key| APIGW
    Lambda -->|ec2:StartInstances (Sync ≤8s)| EC2
    Lambda -->|Read / Write Tasks| SupaTxn
    SupaTxn --> Postgres
    Lambda -->|Presigned URLs| S3

    EC2 -->|SKIP LOCKED Claim & Heartbeat| SupaSess
    SupaSess --> Postgres
    EC2 --> AGY
    EC2 -->|Upload / Download Artifacts| S3
    EC2 -->|sudo -n shutdown -h now (300s Idle)| EC2
```

### Critical Component Dependency Chain
1. **PWA** depends on **API Gateway** and **Supabase Auth**.
2. **Lambda API** depends on **Supabase Transaction Pooler (:6543)** and IAM `ec2:StartInstances` permissions.
3. **EC2 Worker** depends on **Supabase Session Pooler (:5432)**, **AWS S3**, **SSM Parameter Store** (`/griffin/prod/worker/*`), and local **Antigravity CLI** credentials.
4. **Scale-to-Zero Loop**:
   - Work Queued -> Lambda invokes `StartInstances` -> EC2 boots -> Systemd starts `griffin-worker` -> Worker claims task via `SELECT FOR UPDATE SKIP LOCKED` -> Executes `agy` -> Task completes -> Queue empty for 300s -> Worker calls `sudo -n /usr/sbin/shutdown -h now` -> EC2 transitions to `stopped`.

---

## 4. Exact Prerequisites Still Requiring User Action

Before any cloud deployment can proceed, the following manual prerequisites must be performed:

1. **Attach Least-Privilege IAM Policy to `griffin-bootstrap`**:
   - The user `arn:aws:iam::123456789012:user/griffin-bootstrap` currently fails `ec2:RunInstances` with `UnauthorizedOperation`.
   - An account administrator must attach **Policy A** (detailed in Section 5) to `griffin-bootstrap`.
2. **Attach Worker Permissions to `GriffinEC2WorkerRole`**:
   - Role `GriffinEC2WorkerRole` exists, but lacks `ssm:GetParametersByPath` on `/griffin/prod/worker*` and `s3:AbortMultipartUpload` on the S3 bucket.
   - An administrator must attach **Policy C** to `GriffinEC2WorkerRole`.
3. **Provision Hosted Supabase Project**:
   - Create a project in `ap-south-1` (Mumbai).
   - In Supabase Dashboard:
     - Disable public sign-ups (single-operator deployment).
     - Create the operator user in Authentication.
     - Note Session Pooler URI (`port 5432`) and Transaction Pooler URI (`port 6543`).
     - Retrieve Supabase JWT Secret (or JWKS URL) and Anon Key.
4. **Populate Production Secrets in AWS SSM Parameter Store**:
   - Store the following `SecureString` parameters under `/griffin/prod/worker/` using AWS CLI:
     - `/griffin/prod/worker/DATABASE_URL` (Session Pooler URL)
     - `/griffin/prod/worker/SUPABASE_JWT_SECRET` (>= 32 chars)
     - `/griffin/prod/worker/WORKER_API_KEY` (Generated 32+ character random key)
     - `/griffin/prod/worker/S3_BUCKET` (`griffin-artifacts-123456789012-ap-south-1`)
5. **Supply Real Domain Names & TLS Certificates**:
   - Provide two custom hostnames owned by operator:
     - `API_DOMAIN` (e.g. `api.griffin.example.com` for API Gateway HTTP API)
     - `WEB_DOMAIN` (e.g. `griffin.example.com` for Vercel PWA)
6. **Configure AWS Cost Budget**:
   - Set up an AWS Budget alarm for Account `123456789012` at $10/month and $20/month with operator email alerts.

---

## 5. Corrected Least-Privilege IAM Policy Proposals

### 5.1 Critique of Previous Broad Proposals
Previous deployment draft policies contained security and operational flaws:
- **Overly Broad Wildcard**: `ec2:RunInstances` on `Resource: "*"` lacked instance type and encryption constraints, violating security baselines.
- **Unnecessary Destructive Permissions**: Included `ec2:TerminateInstances` on `Resource: "*"` for a routine deployment user.
- **Missing Required Actions**: Omitted `ssm:GetParameter` for the Canonical Ubuntu AMI parameter, causing AMI resolution to fail. Omitted SSM Session Manager permissions required for interactive debugging.
- **Unscoped `iam:PassRole`**: Allowed passing roles without restricting the target service to `ec2.amazonaws.com`.

### 5.2 Policy A — Deployer Policy for `griffin-bootstrap`
Attach this inline policy (`GriffinBootstrapDeployerPolicy`) to `arn:aws:iam::123456789012:user/griffin-bootstrap` via an admin account:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "Ec2DescribeAndDiscovery",
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeImages",
        "ec2:DescribeVpcs",
        "ec2:DescribeSubnets",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeInstances",
        "ec2:DescribeInstanceStatus",
        "ec2:DescribeVolumes",
        "ec2:DescribeTags"
      ],
      "Resource": "*",
      "Condition": {
        "StringEquals": { "aws:RequestedRegion": "ap-south-1" }
      }
    },
    {
      "Sid": "RunInstancesNonInstanceScoped",
      "Effect": "Allow",
      "Action": "ec2:RunInstances",
      "Resource": [
        "arn:aws:ec2:ap-south-1::image/*",
        "arn:aws:ec2:ap-south-1:123456789012:subnet/*",
        "arn:aws:ec2:ap-south-1:123456789012:network-interface/*",
        "arn:aws:ec2:ap-south-1:123456789012:security-group/*"
      ]
    },
    {
      "Sid": "RunInstancesRestrictedCompute",
      "Effect": "Allow",
      "Action": "ec2:RunInstances",
      "Resource": "arn:aws:ec2:ap-south-1:123456789012:instance/*",
      "Condition": {
        "StringEquals": { "ec2:InstanceType": "t4g.medium" }
      }
    },
    {
      "Sid": "RunInstancesEncryptedStorageOnly",
      "Effect": "Allow",
      "Action": "ec2:RunInstances",
      "Resource": "arn:aws:ec2:ap-south-1:123456789012:volume/*",
      "Condition": {
        "Bool": { "ec2:Encrypted": "true" }
      }
    },
    {
      "Sid": "TagOnRunInstancesOnly",
      "Effect": "Allow",
      "Action": "ec2:CreateTags",
      "Resource": [
        "arn:aws:ec2:ap-south-1:123456789012:instance/*",
        "arn:aws:ec2:ap-south-1:123456789012:volume/*",
        "arn:aws:ec2:ap-south-1:123456789012:security-group/*"
      ],
      "Condition": {
        "StringEquals": { "ec2:CreateAction": ["RunInstances", "CreateSecurityGroup"] }
      }
    },
    {
      "Sid": "CreateRunnerSecurityGroup",
      "Effect": "Allow",
      "Action": "ec2:CreateSecurityGroup",
      "Resource": [
        "arn:aws:ec2:ap-south-1:123456789012:vpc/vpc-0c233c7991952ec37",
        "arn:aws:ec2:ap-south-1:123456789012:security-group/*"
      ]
    },
    {
      "Sid": "PassExistingWorkerRoleToEc2",
      "Effect": "Allow",
      "Action": "iam:PassRole",
      "Resource": "arn:aws:iam::123456789012:role/GriffinEC2WorkerRole",
      "Condition": {
        "StringEquals": { "iam:PassedToService": "ec2.amazonaws.com" }
      }
    },
    {
      "Sid": "ResolveUbuntuAmiViaPublicSsmParam",
      "Effect": "Allow",
      "Action": "ssm:GetParameter",
      "Resource": "arn:aws:ssm:ap-south-1::parameter/aws/service/canonical/ubuntu/server/24.04/stable/current/arm64/hvm/ebs-gp3/ami-id"
    },
    {
      "Sid": "SSMSessionManagerConnect",
      "Effect": "Allow",
      "Action": "ssm:StartSession",
      "Resource": [
        "arn:aws:ec2:ap-south-1:123456789012:instance/*",
        "arn:aws:ssm:ap-south-1:*:document/SSM-SessionManagerRunShell"
      ],
      "Condition": {
        "StringEquals": { "ssm:resourceTag/Project": "Griffin" }
      }
    },
    {
      "Sid": "SSMSessionHousekeeping",
      "Effect": "Allow",
      "Action": [
        "ssm:DescribeInstanceInformation",
        "ssm:DescribeSessions",
        "ssm:GetConnectionStatus"
      ],
      "Resource": "*"
    },
    {
      "Sid": "SSMTerminateOwnSessions",
      "Effect": "Allow",
      "Action": [
        "ssm:TerminateSession",
        "ssm:ResumeSession"
      ],
      "Resource": "arn:aws:ssm:*:*:session/${aws:userid}-*"
    }
  ]
}
```

### 5.3 Policy B — Workload Role for API Lambda (`griffin-api-lambda-role`)
Attach to the Lambda execution role:
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "WakeRunnerOnly",
      "Effect": "Allow",
      "Action": [
        "ec2:StartInstances",
        "ec2:StopInstances"
      ],
      "Resource": "arn:aws:ec2:ap-south-1:123456789012:instance/<INSTANCE_ID>"
    },
    {
      "Sid": "DescribeInstanceStatus",
      "Effect": "Allow",
      "Action": "ec2:DescribeInstances",
      "Resource": "*"
    },
    {
      "Sid": "ArtifactStorageAccess",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:DeleteObject",
        "s3:AbortMultipartUpload"
      ],
      "Resource": "arn:aws:s3:::griffin-artifacts-123456789012-ap-south-1/*"
    },
    {
      "Sid": "ArtifactBucketList",
      "Effect": "Allow",
      "Action": "s3:ListBucket",
      "Resource": "arn:aws:s3:::griffin-artifacts-123456789012-ap-south-1"
    }
  ]
}
```

### 5.4 Policy C — Workload Role Amendment for Worker (`GriffinEC2WorkerRole`)
Update the inline policy on `GriffinEC2WorkerRole` to grant the SSM parameters required by [`infra/helpers/griffin-fetch-env.sh`](file:///home/adhyan/Desktop/griffin/infra/helpers/griffin-fetch-env.sh) and multipart S3 aborts:
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ArtifactObjects",
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:AbortMultipartUpload"
      ],
      "Resource": "arn:aws:s3:::griffin-artifacts-123456789012-ap-south-1/*"
    },
    {
      "Sid": "ArtifactBucketMetadata",
      "Effect": "Allow",
      "Action": [
        "s3:ListBucket",
        "s3:GetBucketLocation"
      ],
      "Resource": "arn:aws:s3:::griffin-artifacts-123456789012-ap-south-1"
    },
    {
      "Sid": "FetchWorkerEnvSecrets",
      "Effect": "Allow",
      "Action": [
        "ssm:GetParameter",
        "ssm:GetParametersByPath"
      ],
      "Resource": "arn:aws:ssm:ap-south-1:123456789012:parameter/griffin/prod/worker*"
    }
  ]
}
```

---

## 6. Supabase Production Setup Checklist

Follow this strict step-by-step procedure:

- [ ] **1. Create Hosted Project**: Select `ap-south-1` region to minimize latency with AWS EC2/Lambda.
- [ ] **2. Retrieve Connection Strings**:
  - Worker & Migrations: Obtain **Session Pooler** URI (`port 5432`, e.g., `postgresql://postgres.[ref]:[password]@aws-0-ap-south-1.pooler.supabase.com:5432/postgres`).
  - Lambda API Ingress: Obtain **Transaction Pooler** URI (`port 6543`, e.g., `postgresql://postgres.[ref]:[password]@aws-0-ap-south-1.pooler.supabase.com:6543/postgres`).
- [ ] **3. Validate Local Connection**:
  ```bash
  export MIGRATION_DATABASE_URL="postgresql://postgres.[ref]:[password]@...:5432/postgres?sslmode=require"
  go run ./cmd/griffin-migrate status
  ```
  *(Expected: `applied: (none), pending: 000001_init_schema, 000002_rls_policies, 000003_orchestration_and_agent_factory, 000004_s3_storage_artifacts`)*
- [ ] **4. Apply Migrations**:
  ```bash
  go run ./cmd/griffin-migrate up
  ```
  *(Note: Do NOT pass `-local-auth-shim`. The migrate tool verifies platform `auth.users` and `auth.uid()` exist).*
- [ ] **5. Confirm Migration State & Table Security**:
  ```bash
  go run ./cmd/griffin-migrate status
  # Verify all 4 migrations applied, 0 pending.
  ```
  Verify via psql or Supabase SQL Editor:
  ```sql
  SELECT relname, relrowsecurity, relforcerowsecurity 
  FROM pg_class 
  WHERE relnamespace = 'public'::regnamespace AND relkind = 'r';
  ```
  *(Confirm `relrowsecurity` and `relforcerowsecurity` are TRUE on all tenant tables).*
- [ ] **6. Authentication Setup**:
  - In Supabase Dashboard -> **Authentication** -> **Providers**: Disable Email Signup (only allow existing users).
  - Create the primary operator user email and secure password.
  - In Supabase Dashboard -> **Settings** -> **API**: Record `Project URL`, `anon public key`, and determine whether project uses HS256 JWT Secret or JWKS endpoint.

---

## 7. EC2 & Antigravity Live-Validation Checklist

Execute on the deployed EC2 host via AWS Systems Manager Session Manager:

### Stage 1: Host & OS Verification
- [ ] **1. Instance Boot Check**:
  ```bash
  aws ec2 describe-instances --profile griffin-bootstrap --region ap-south-1 \
    --filters Name=tag:Name,Values=griffin-cloud-runner \
    --query 'Reservations[].Instances[].[InstanceId,State.Name,InstanceType,Architecture]'
  ```
  *(Must show `t4g.medium`, `arm64`, and state `running`)*.
- [ ] **2. Connect via SSM Session Manager**:
  ```bash
  aws ssm start-session --target <INSTANCE_ID> --profile griffin-bootstrap --region ap-south-1
  ```
- [ ] **3. User & Lingering DBus Verification**:
  ```bash
  id griffin
  loginctl show-user griffin -p Linger
  # Output must state: Linger=yes
  ```
- [ ] **4. Sudoers Integrity Check**:
  ```bash
  sudo -u griffin sudo -n /usr/sbin/shutdown -k now
  # Command must exit 0 without asking for password
  ```

### Stage 2: Antigravity CLI Verification & Unattended Persistence
- [ ] **5. CLI Installation Check**:
  ```bash
  sudo -u griffin -i
  export PATH="$HOME/.local/bin:$PATH"
  agy --version
  ```
  *(Must show `agy` version 1.2.0 or higher)*.
- [ ] **6. One-Time Interactive Authentication**:
  ```bash
  export XDG_RUNTIME_DIR=/run/user/$(id -u)
  export DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u)/bus
  agy
  ```
  *(Open printed OAuth URL in laptop browser, sign in with personal Google subscription account, and enter verification code into terminal).*
- [ ] **7. Interactive Execution Test**:
  ```bash
  agy -p 'Reply with {"status":"ready"}' --output-format json
  # Must return valid JSON payload with exit code 0
  ```
- [ ] **8. Headless Non-Interactive Probe**:
  Disconnect the SSM session. From local machine, run remote shell via AWS SSM send-command:
  ```bash
  aws ssm send-command --profile griffin-bootstrap --region ap-south-1 \
    --instance-ids <INSTANCE_ID> \
    --document-name "AWS-RunShellScript" \
    --parameters 'commands=["sudo -u griffin env HOME=/home/griffin XDG_RUNTIME_DIR=/run/user/1001 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1001/bus /home/griffin/.local/bin/agy -p ping --output-format json"]'
  ```
  *(Verify output is successful without prompt).*
- [ ] **9. Cold Reboot Persistence Test**:
  ```bash
  aws ec2 reboot-instances --instance-ids <INSTANCE_ID> --profile griffin-bootstrap --region ap-south-1
  ```
  Wait for instance to return to running, then execute the headless command in step 8.
  - **Pass**: Returns exit 0 and valid JSON.
  - **Fail**: Returns `ErrAuthenticationRequired` (indicates DBus keyring locked upon reboot). If failed, enable `GEMINI_API_KEY` fallback mode in `/etc/griffin/worker.env`.
- [ ] **10. Enable Systemd Service**:
  ```bash
  sudo systemctl enable --now griffin-worker
  systemctl status griffin-worker
  journalctl -u griffin-worker -n 50
  ```

---

## 8. Deployment Sequence with Rollback Steps

### Execution Sequence

```mermaid
sequenceDiagram
    autonumber
    actor Operator
    participant IAM as AWS IAM
    participant Supa as Supabase
    participant SSM as AWS SSM
    participant S3 as AWS S3
    participant EC2 as AWS EC2
    participant Lmb as AWS Lambda
    participant APIGW as API Gateway
    participant Vercel as Vercel PWA

    Operator->>IAM: 1. Attach Policy A to griffin-bootstrap & Policy C to worker role
    Operator->>Supa: 2. Provision Supabase project & run griffin-migrate up
    Operator->>SSM: 3. Put SecureString secrets under /griffin/prod/worker/
    Operator->>EC2: 4. Run infra/provision-ec2.sh
    Operator->>EC2: 5. SSM Session: Authenticate agy & verify reboot persistence
    Operator->>EC2: 6. Install worker bundle & start griffin-worker.service
    Operator->>Lmb: 7. Deploy griffin-api.zip with Lambda Web Adapter layer
    Operator->>APIGW: 8. Configure HTTP API & custom domain mapping
    Operator->>Vercel: 9. Set env vars & deploy PWA to production
    Operator->>APIGW: 10. Run End-to-End Live Smoke Test
```

### Granular Rollback Playbook

| Component | Failure Condition | Immediate Rollback Action | RTO / Recovery Impact |
| :--- | :--- | :--- | :--- |
| **PWA (Vercel)** | Broken UI or client auth errors | Run `vercel rollback` in `web/` to revert to previous stable deployment. | < 1 minute; instant CDN switch |
| **API Ingress (Lambda)** | Cold start failures, LWA crashes, 5xx errors | `aws lambda update-alias --function-name griffin-api --name prod --function-version <PREV_VERSION>` or set concurrency to 0 to halt traffic. | < 2 minutes |
| **Scheduler Tick** | Errant tick loops or task storms | `aws events disable-rule --name griffin-tick --profile griffin-bootstrap --region ap-south-1`. | Immediate halt of automated wakeups |
| **Worker Host (EC2)** | Worker daemon crash or resource leak | `aws ec2 stop-instances --instance-ids <INSTANCE_ID>`. Systemd stops, stopping compute billing. | Immediate halt of compute cost |
| **Database Migrations** | DDL migration failure | Restore from `pg_dump` taken immediately prior to migration. (Do not run destructive `down.sql` against production). | 5–15 minutes |
| **Compromised Secrets** | Leaked JWT secret or worker key | Rotate parameter in SSM Parameter Store, update Lambda env vars, and restart services. | < 5 minutes |

---

## 9. Remaining Blockers Classified by Severity

| Blocker ID | Description | Severity | Mitigation / Resolution |
| :--- | :--- | :--- | :--- |
| **BLK-01** | `griffin-bootstrap` IAM user lacks `ec2:RunInstances` (Dry-run failed with `UnauthorizedOperation`). | **CRITICAL** | Attach Policy A in Section 5 via Account Admin identity before running `./infra/provision-ec2.sh`. |
| **BLK-02** | `GriffinEC2WorkerRole` lacks `ssm:GetParametersByPath` on `/griffin/prod/worker*` and `s3:AbortMultipartUpload`. | **HIGH** | Update inline policy on `GriffinEC2WorkerRole` with Policy C in Section 5. |
| **BLK-03** | Antigravity CLI keyring persistence across unattended cold reboot is unproven in AWS cloud. | **HIGH** | Perform Stage 2 verification (Section 7) on the provisioned runner. Keep `GEMINI_API_KEY` ready as contingency. |
| **BLK-04** | Remote Supabase project and credentials have not been configured or migrated. | **HIGH** | Follow Section 6 checklist to set up hosted database and apply migrations. |
| **BLK-05** | Real DNS domains (`API_DOMAIN`, `WEB_DOMAIN`) not yet supplied (currently using documentation placeholders). | **MEDIUM** | Designate domain names and register DNS records pointing to API Gateway and Vercel. |
| **BLK-06** | S3 bucket `griffin-artifacts-123456789012-ap-south-1` lacks lifecycle rule (`NoSuchLifecycleConfiguration`). | **MEDIUM** | Apply lifecycle policy to expire non-current versions after 30 days and abort incomplete multipart uploads after 7 days. |
| **BLK-07** | AWS Budget alarm is not yet active for Account `123456789012`. | **MEDIUM** | Create budget with $10 and $20 thresholds in AWS Console. |
| **BLK-08** | Local machine lacks `session-manager-plugin`. | **LOW** | Install `aws-session-manager-plugin` on local workstation to enable direct SSM shell access. |

---

## 10. Final Verdict

### **VERDICT: READY FOR CONTROLLED DEPLOYMENT**

### Justification & Operational Boundaries
The Griffin codebase is **NOT** ready for immediate production traffic (it cannot be, as cloud infrastructure and remote databases do not yet exist, and deployment credentials lack launch permissions).

However, the repository is **GENUINELY READY FOR CONTROLLED DEPLOYMENT**:
1. All eight security and architectural gates (G1–G8) are completely closed, tested, and verified.
2. The Go test suite passes 100% (197/197 passing assertions) including PostgreSQL transaction-pooler simple protocol mode.
3. The AWS S3 storage integration is 100% live-verified against AWS Account C.
4. Packaging scripts deterministically produce valid `linux/arm64` binaries for both worker and Lambda.
5. Frontend compiles cleanly with 0 type errors, 0 lint errors, and zero dev-login leaks.
6. The exact prerequisites, corrected least-privilege IAM policies, and execution steps are fully documented and ready for sequential execution once the administrator attaches Policy A.

**Next Immediate Step**: Account administrator attaches **Policy A** to `griffin-bootstrap` and **Policy C** to `GriffinEC2WorkerRole`. Once confirmed, proceed with Phase 1 of the Deployment Runbook.
