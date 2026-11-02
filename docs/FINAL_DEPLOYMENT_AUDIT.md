# Griffin Final Pre-Deployment Infrastructure & Application Audit

This document records the comprehensive pre-deployment audit conducted across the Griffin repository, runtime dependencies, AWS Account C (`220115213279`), and the Next.js 15 PWA web console.

---

## 1. Local Tooling & System Environment

| Tool / Runtime | Installed Version | Verification Command | Status |
| :--- | :--- | :--- | :--- |
| **Operating System** | Linux x86_64 (Kernel 7.2.4-arch1-2) | `uname -a` | Verified |
| **Go Compiler** | `go1.27.1-X:nodwarf5 linux/amd64` | `go version` | Verified |
| **Node.js** | `v24.20.0` | `node -v` | Verified |
| **npm** | `11.19.0` | `npm -v` | Verified |
| **AWS CLI** | `aws-cli/2.36.44 Python/3.14.6` | `aws --version` | Verified |
| **Google Antigravity CLI** | `1.2.0` (`agy`) | `agy --version` | Verified |
| **Git Working Tree** | Clean (`master` branch) | `git status` | Clean |

---

## 2. Test Suite & Code Quality Audit

### 2.1 Backend (Go 1.27.1)
- **Formatting**: `gofmt -l .` — Clean (0 unformatted files).
- **Static Analysis**: `go vet ./internal/... ./cmd/... ./tests/...` — Clean (0 warnings or errors).
- **Unit & Integration Tests**: `go test -count=1 ./internal/... ./cmd/... ./tests/...` — **100% PASS** (77/77 tests passing).
- **Live AWS S3 Integration Test**: `TestS3_LiveAWSVerification` in [`tests/s3_live_test.go`](file:///home/adhyan/Desktop/griffin/tests/s3_live_test.go) — **PASS (0.79s)** directly against AWS S3 bucket `griffin-artifacts-220115213279-ap-south-1` in `ap-south-1`.

### 2.2 Frontend (Next.js 16.4.3 / React 19 / TypeScript 5)
- **TypeScript Typecheck**: `cd web && npx tsc --noEmit` — Clean (0 errors).
- **ESLint Code Quality**: `cd web && npm run lint` — Clean (0 errors).
- **Production Build**: `cd web && npm run build` — Clean (All 10 management screens compiled).
- **Dev-Login Production Bundle Leak Check**: Verified `.next/static` contains **0** occurrences of dev-login endpoints or strings when `NEXT_PUBLIC_ENABLE_DEV_LOGIN=false`.

---

## 3. AWS Account C Discovery & Resource Inventory

- **Target Account**: `220115213279`
- **Target Region**: `ap-south-1` (Mumbai)
- **Caller Identity**: `arn:aws:iam::220115213279:user/griffin-bootstrap`

### 3.1 Object Storage (S3)
- **Bucket**: `griffin-artifacts-220115213279-ap-south-1`
- **Location**: `ap-south-1`
- **Public Access Block**: `BlockPublicAcls=true`, `IgnorePublicAcls=true`, `BlockPublicPolicy=true`, `RestrictPublicBuckets=true` (100% Private)
- **Encryption**: `AES256` default SSE enabled; `SSE-C` blocked.
- **Verification**: Live verified for upload, download, metadata head, presigned URL retrieval, and deletion.

### 3.2 IAM Runtime Roles & Profiles
- **Role**: `GriffinEC2WorkerRole` (`arn:aws:iam::220115213279:role/GriffinEC2WorkerRole`)
- **Instance Profile**: `GriffinEC2WorkerProfile` (`arn:aws:iam::220115213279:instance-profile/GriffinEC2WorkerProfile`)
- **Trust Policy**: Allows `ec2.amazonaws.com` to assume role.
- **Attached Policies**:
  - `arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore`
  - Inline Policy `GriffinArtifactStorageAccess`: Scoped exclusively to `arn:aws:s3:::griffin-artifacts-220115213279-ap-south-1/*` and `arn:aws:s3:::griffin-artifacts-220115213279-ap-south-1`.

### 3.3 Compute Networking (VPC & Subnets)
- **VPC ID**: `vpc-0c233c7991952ec37` (Default VPC, `172.31.0.0/16`)
- **Subnets**:
  - `subnet-066c528186f92acaa` (`ap-south-1a`, 4091 available IPs)
  - `subnet-0fd6564f579517c78` (`ap-south-1b`, 4091 available IPs)
  - `subnet-04fd2a9ff8075818a` (`ap-south-1c`, 4091 available IPs)
- **Security Groups**:
  - `sg-0ba229ab02d509fd8` (Default SG)
  - `griffin-runner-sg`: Automated creation staged in [`infra/provision-ec2.sh`](file:///home/adhyan/Desktop/griffin/infra/provision-ec2.sh) with zero inbound ports.

---

## 4. Empirical Security & Deployment Blockers

### Blocker 1: Missing EC2 Launch Permissions for `griffin-bootstrap`
- **Issue**: Attempting dry-run invocation of `aws ec2 run-instances` fails with `UnauthorizedOperation`. The IAM user `griffin-bootstrap` does not have `ec2:RunInstances` or `ec2:CreateSecurityGroup`.
- **Impact**: Automatic launching of the EC2 worker instance via CLI requires an attached policy in Account C.
- **Proposed Least-Privilege IAM Policy**:
  ```json
  {
    "Version": "2012-10-17",
    "Statement": [
      {
        "Sid": "GriffinEC2Lifecycle",
        "Effect": "Allow",
        "Action": [
          "ec2:RunInstances",
          "ec2:StartInstances",
          "ec2:StopInstances",
          "ec2:TerminateInstances",
          "ec2:CreateSecurityGroup",
          "ec2:CreateTags",
          "ec2:AuthorizeSecurityGroupEgress"
        ],
        "Resource": "*"
      },
      {
        "Sid": "GriffinPassRoleToWorker",
        "Effect": "Allow",
        "Action": "iam:PassRole",
        "Resource": "arn:aws:iam::220115213279:role/GriffinEC2WorkerRole"
      }
    ]
  }
  ```
- **Verification Command Post-Approval**:
  ```bash
  AWS_PROFILE=griffin-bootstrap aws ec2 run-instances \
    --region ap-south-1 \
    --image-id ami-072d8688e7b3aba7a \
    --instance-type t4g.medium \
    --iam-instance-profile Name=GriffinEC2WorkerProfile \
    --dry-run
  ```

---

## 5. Cost Projections for Cloud Runner

- **Instance Type**: `t4g.medium` (2 vCPUs, 4 GiB memory, ARM64 Graviton2).
- **On-Demand Hourly Rate**: $0.0336 / hour in `ap-south-1`.
- **Expected Daily Usage**: ~2 hours/day on-demand with scale-to-zero.
- **Estimated Monthly Compute Cost**: 60 hours * $0.0336 = **$2.02 / month**.
- **EBS Storage**: 30 GiB gp3 encrypted = **$2.40 / month**.
- **S3 & Network Egress**: <**$0.20 / month**.
- **Maximum 24/7 Exposure**: 720 hours * $0.0336 + $2.40 = **$26.59 / month** (prevented by `IdleShutdownMonitor` stopping host after 10m idle).
