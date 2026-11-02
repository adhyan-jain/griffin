# Griffin Cloud Deployment & Operations Runbook

This document details the end-to-end production deployment, cloud provisioning, and operational procedures for Griffin across **AWS Account C** (`123456789012`), **Supabase PostgreSQL**, and the **Next.js 15 PWA Web Console**.

---

## 1. Cloud Architecture Topology

Griffin is designed around **Scale-to-Zero Cloud Execution**:

```
                                  +------------------------------------+
                                  |      Next.js 15 PWA Console        |
                                  |   (All 10 Management Screens)      |
                                  +-----------------+------------------+
                                                    | HTTP / JWT
                                                    v
+------------------------+        +-----------------+------------------+
| EventBridge Scheduler  |------->|      Griffin API Ingress Server    |
| (POST /scheduler/tick) |        |      (Lightweight HTTP / Systemd)  |
+------------------------+        +--------+------------------+--------+
                                           |                  |
                       +-------------------+                  +--------------------+
                       | SQL (Pooler)                         | Wake / Sleep       |
                       v                                      v                    v
         +-------------+-------------+          +-------------+-------+    +-------+--------------+
         |    Supabase PostgreSQL    |          | AWS EC2 Scale-to-0  |    | AWS S3 Bucket        |
         | (Durable Queue & RLS)     |          | Runner (t4g.medium) |    | griffin-artifacts-   |
         |                           |          | Antigravity (agy)   |    | 123456789012-        |
         |                           |          | Zero Inbound Ports  |    | ap-south-1           |
         +---------------------------+          +---------------------+    +----------------------+
```

1. **Supabase PostgreSQL**: Operational source of truth, durable task state machine, task leases, and RLS tenant boundaries.
2. **AWS S3 Object Storage**: Immutable, encrypted private file storage (`griffin-artifacts-123456789012-ap-south-1` in `ap-south-1`).
3. **API Ingress**: Always-on lightweight Go binary handling user requests, orchestrator submissions, approvals, and runner wake-up.
4. **EC2 Scale-to-Zero Cloud Runner**: On-demand compute (`t4g.medium` Graviton2) executing tasks via Google Antigravity CLI (`agy`), shutting down after 10 minutes of complete queue idleness.
5. **EventBridge Scheduler**: Invokes `POST /api/v1/scheduler/tick` every minute to trigger scheduled work and evaluate due tasks.
6. **Next.js 15 PWA Console**: Modern responsive web app in `web/` with 10 dedicated management screens.

---

## 2. Infrastructure Configuration & Verification

### 2.1 AWS Account C Identity
- **AWS Account ID**: `123456789012`
- **Primary Region**: `ap-south-1` (Mumbai)
- **Local AWS CLI Profile**: `griffin-bootstrap`
- **S3 Bucket**: `griffin-artifacts-123456789012-ap-south-1` (Encrypted AES256, BlockPublicAccess Enabled, BucketOwnerEnforced)

### 2.2 IAM Permissions Required for `griffin-bootstrap`

To provision and launch EC2 instances on Account C, the following IAM policy statement must be attached to the `griffin-bootstrap` IAM user / role in the AWS Management Console:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "GriffinEC2LifecycleManagement",
      "Effect": "Allow",
      "Action": [
        "ec2:RunInstances",
        "ec2:DescribeInstances",
        "ec2:DescribeImages",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeVpcs",
        "ec2:CreateSecurityGroup",
        "ec2:CreateTags",
        "ec2:StartInstances",
        "ec2:StopInstances",
        "ec2:TerminateInstances"
      ],
      "Resource": "*"
    },
    {
      "Sid": "GriffinPassWorkerRole",
      "Effect": "Allow",
      "Action": [
        "iam:PassRole"
      ],
      "Resource": "arn:aws:iam::123456789012:role/GriffinEC2*"
    }
  ]
}
```

---

## 3. Database Migration Runbook

All database migrations must be applied in sequential order.

### Using Make:
```bash
# Target local or remote Supabase instance
make migrate
```

### Manual psql execution against Supabase:
```bash
PGPASSWORD="$SUPABASE_DB_PASSWORD" psql -h "$SUPABASE_DB_HOST" -p 5432 -U postgres -d postgres -f migrations/000001_init_schema.up.sql
PGPASSWORD="$SUPABASE_DB_PASSWORD" psql -h "$SUPABASE_DB_HOST" -p 5432 -U postgres -d postgres -f migrations/000002_rls_policies.up.sql
PGPASSWORD="$SUPABASE_DB_PASSWORD" psql -h "$SUPABASE_DB_HOST" -p 5432 -U postgres -d postgres -f migrations/000003_orchestration_and_agent_factory.up.sql
PGPASSWORD="$SUPABASE_DB_PASSWORD" psql -h "$SUPABASE_DB_HOST" -p 5432 -U postgres -d postgres -f migrations/000004_s3_storage_artifacts.up.sql
```

---

## 4. EC2 Cloud Runner Provisioning

1. Run the provisioning script:
   ```bash
   AWS_PROFILE=griffin-bootstrap ./infra/provision-ec2.sh
   ```
2. The script executes the following:
   - Resolves the official Ubuntu 24.04 ARM64 AMI (`ami-0d8b4e4e9a59cf861`).
   - Creates IAM Role `GriffinEC2WorkerRole` with `AmazonSSMManagedInstanceCore` and S3 artifact read/write permissions.
   - Configures Security Group `griffin-runner-sg` with **zero inbound ports** (port 22 closed).
   - Launches `t4g.medium` instance with 30 GiB encrypted root volume.
   - Executes cloud-init (`infra/user-data.sh`) creating the `griffin` system user, lingering DBus session, and installing the Antigravity CLI.

3. Connect securely via AWS SSM (no bastion or SSH keys required):
   ```bash
   aws ssm start-session --target <INSTANCE_ID> --region ap-south-1 --profile griffin-bootstrap
   ```

---

## 5. EventBridge Scheduler Configuration

To invoke the background scheduler tick:

1. Create an EventBridge Schedule targeting the API server:
   ```bash
   aws scheduler create-schedule \
     --name "griffin-scheduler-tick" \
     --schedule-expression "rate(1 minute)" \
     --target '{
       "Arn": "arn:aws:scheduler:::aws-sdk:http:invoke",
       "Input": "{\"method\": \"POST\", \"url\": \"https://api.griffin.workforce/api/v1/scheduler/tick\", \"headers\": {\"X-Worker-Key\": \"'$GRIFFIN_WORKER_API_KEY'\"}}",
       "RoleArn": "arn:aws:iam::123456789012:role/GriffinSchedulerRole"
     }' \
     --region ap-south-1 \
     --profile griffin-bootstrap
   ```

---

## 6. Next.js 15 PWA Frontend Deployment

The Next.js 15 TypeScript PWA in `web/` can be built and deployed via Docker or standard Node.js hosting:

### Local / Production Build:
```bash
cd web
npm install --ignore-scripts
npm run build
npm start -p 3000
```

### Environment Variables (`web/.env.production`):
```env
NEXT_PUBLIC_API_URL=https://api.griffin.workforce
```

### Running with Docker:
```dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm install --ignore-scripts
COPY . .
RUN npm run build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json ./package.json

EXPOSE 3000
CMD ["npm", "start"]
```

---

## 7. Verification Checklist

| Component | Verification Command | Expected Result |
| :--- | :--- | :--- |
| **API Health** | `curl -f http://localhost:8080/healthz` | `{"status":"ok", ...}` |
| **Database Ping** | `curl -f http://localhost:8080/readyz` | `{"database":"connected", ...}` |
| **Runner State** | `curl -f http://localhost:8080/api/v1/runner/status` | `{"state":"running", ...}` |
| **Live AWS S3** | `GRIFFIN_ENABLE_LIVE_S3_TEST=true go test -v ./tests -run TestS3_LiveAWSVerification` | `PASS` in ~0.8s |
| **Web Build** | `cd web && npm run build` | 10 screens compiled with 0 errors |
