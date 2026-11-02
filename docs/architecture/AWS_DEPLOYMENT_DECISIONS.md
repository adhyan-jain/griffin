# AWS Deployment Decisions & Technical Specifications

## 1. Core AWS Technical Decisions

### Decision 1: Instance Sizing & Architecture Lifecycle
- **Initial Deployment (Credit Period)**: **`t4g.medium`** (2 vCPUs, 4 GiB RAM, Graviton2 ARM64) in `ap-south-1a`.
- **Post-Credit Operating State**: Downsize to **`t4g.small`** (2 vCPUs, 2 GiB RAM) with a 2 GiB swapfile enabled.
- **Operating System**: **Ubuntu 24.04 LTS Server (64-bit ARM / `arm64`)**.

---

### Decision 2: Zero-SSH Remote Administration via AWS SSM Session Manager
- **Context**: Opening inbound SSH port 22 to the public internet creates continuous exposure to brute-force authentication bots and necessitates managing private `.pem` key pairs.
- **Decision**: **Inbound Port 22 is completely closed in the Security Group.** All administrative terminal access occurs via **AWS Systems Manager (SSM) Session Manager**.
- **Implementation**:
  - The EC2 instance is launched with an IAM Instance Profile containing the AWS-managed policy `AmazonSSMManagedInstanceCore`.
  - The Ubuntu 24.04 AMI includes the `amazon-ssm-agent` pre-installed and enabled.
  - Administration from your local machine:
    ```bash
    aws ssm start-session --target <instance-id> --region ap-south-1
    ```
  - Port forwarding (e.g. for temporary local debugging of backend port 8080):
    ```bash
    aws ssm start-session --target <instance-id> \
      --document-name AWS-StartPortForwardingSession \
      --parameters '{"portNumber":["8080"],"localPortNumber":["8080"]}'
    ```

---

### Decision 3: Ingress & HTTPS Termination Strategy

We define two viable ingress patterns, with **Option B (Cloudflare Tunnel)** recommended for cost and zero-port security:

#### Option A: Caddy Reverse Proxy with Elastic IP
- **Security Group**:
  - Inbound: Port 80 (HTTP) and Port 443 (HTTPS) open to `0.0.0.0/0`.
  - Inbound: Port 22 (SSH) **DENIED / CLOSED**.
  - Outbound: All traffic allowed (`0.0.0.0/0`).
- **Software**: **Caddy v2** running as a systemd service. Caddy handles automatic Let's Encrypt TLS certificate issuance and renewal with zero human maintenance.
- **Caddyfile** (`/etc/caddy/Caddyfile`):
  ```caddy
  api.griffin.yourdomain.com {
      reverse_proxy localhost:8080 {
          header_up X-Real-IP {remote_host}
          header_up X-Forwarded-Proto {scheme}
      }
  }
  ```
- **Cost**: Incurs $3.65/mo for the Public IPv4 address.

#### Option B: Cloudflare Tunnel (`cloudflared`) — RECOMMENDED
- **Security Group**:
  - Inbound: **ZERO INBOUND PORTS OPEN** (No 80, 443, or 22).
  - Outbound: Port 443 to Cloudflare Edge.
- **Software**: `cloudflared` daemon running as a systemd service on EC2.
- **Benefits**:
  - Eliminates the $3.65/mo AWS Public IPv4 fee completely.
  - Complete immunity against port scans and DDoS attacks on the EC2 instance.
  - TLS terminated at Cloudflare Edge with automatic origin encryption.

---

### Decision 4: IAM Least-Privilege Instance Profile

The EC2 instance is assigned an IAM Role `GriffinBackendEC2Role` with an attached inline policy:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "SSMCoreAccess",
      "Effect": "Allow",
      "Action": [
        "ssm:DescribeAssociation",
        "ssm:GetDeployablePatchSnapshotForInstance",
        "ssm:GetDocument",
        "ssm:DescribeDocument",
        "ssm:GetManifest",
        "ssm:GetParameter",
        "ssm:GetParameters",
        "ssm:ListAssociations",
        "ssm:ListInstanceAssociations",
        "ssm:PutInventory",
        "ssm:PutComplianceItems",
        "ssm:PutConfigurePackageResult",
        "ssm:UpdateAssociationStatus",
        "ssm:UpdateInstanceAssociationStatus",
        "ssm:UpdateInstanceInformation",
        "ssmmessages:CreateControlChannel",
        "ssmmessages:CreateDataChannel",
        "ssmmessages:OpenControlChannel",
        "ssmmessages:OpenDataChannel",
        "ec2messages:AcknowledgeMessage",
        "ec2messages:DeleteMessage",
        "ec2messages:FailMessage",
        "ec2messages:GetEndpoint",
        "ec2messages:GetMessages",
        "ec2messages:SendReply"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ParameterStoreSecretsRead",
      "Effect": "Allow",
      "Action": [
        "ssm:GetParameter",
        "ssm:GetParameters",
        "ssm:GetParametersByPath"
      ],
      "Resource": "arn:aws:ssm:ap-south-1:*:parameter/griffin/prod/*"
    }
  ]
}
```

---

### Decision 5: Secret Management via SSM Parameter Store
- Application secrets (Supabase Service Role Key, Database URL, Master Encryption Key, AI Provider API keys) are stored in **AWS Systems Manager Parameter Store** as `SecureString` types under the path `/griffin/prod/`.
- Standard Parameter Store parameters are **FREE** (up to 10,000 parameters, no hourly cost).
- At systemd service startup, a small boot script fetches secrets into an environment file (`/etc/griffin/griffin.env`, permissions `0600 root:root`) read by `griffin.service`.

---

### Decision 6: Automated Backup Strategy (Amazon DLM)
- Database backups remain managed on Supabase (daily WAL backups with PITR).
- EC2 EBS root volume (30 GB gp3) is tagged: `Project=Griffin`, `Environment=Production`.
- **Amazon Data Lifecycle Manager (DLM)** EBS Snapshot Lifecycle Policy:
  - Schedule: Daily at 03:00 UTC (08:30 IST).
  - Retention: Keep the last 7 daily snapshots.
  - Encryption: Enabled (AWS KMS default key).
  - Monthly snapshot storage cost: ~$0.50/month.

---

### Decision 7: Service Supervision (systemd)

The Go modular monolith is installed as a native Linux service supervised by `systemd`:

`/etc/systemd/system/griffin.service`:
```ini
[Unit]
Description=Griffin Core Monolith Service
After=network.target

[Service]
Type=simple
User=griffin
Group=griffin
WorkingDirectory=/opt/griffin
ExecStart=/opt/griffin/bin/griffin-core
Restart=always
RestartSec=5s
EnvironmentFile=/etc/griffin/griffin.env

# Hardening / Sandboxing
ProtectSystem=full
ProtectHome=true
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

---

## 2. Infrastructure Setup Sequence (For Phase 5 Deployment)

When implementation reaches the deployment phase, setup executes in the following sequence:

1. **IAM Role Setup**: Create `GriffinBackendEC2Role` with `AmazonSSMManagedInstanceCore` and Parameter Store read policy; create instance profile.
2. **Security Group Setup**: Create `griffin-backend-sg` in default VPC (`ap-south-1`).
   - If Option A: Inbound 80, 443 from `0.0.0.0/0`.
   - If Option B (Cloudflare Tunnel): **No inbound rules**.
3. **Launch EC2 Instance**:
   - AMI: Ubuntu Server 24.04 LTS (HVM), SSD Volume Type (`arm64`).
   - Type: `t4g.medium`.
   - Storage: 30 GiB `gp3` encrypted.
   - Tag: `Name=griffin-backend`, `Project=Griffin`.
   - User Data script: Installs Caddy / `cloudflared`, sets up user `griffin`, enables swapfile.
4. **Configure Secrets**: Store configuration in AWS SSM Parameter Store (`/griffin/prod/*`).
5. **Configure DLM Snapshot Lifecycle**: Create daily EBS snapshot policy with 7-day retention.
6. **Configure AWS Budgets**: Create $30.00/mo budget with alerts at 50%, 80%, and 100%.
7. **Deploy Go Binary**: Cross-compile Go binary (`GOOS=linux GOARCH=arm64 go build -o griffin-core ./cmd/server`) and deploy to `/opt/griffin/bin/`.
8. **Start & Verify Service**: Start `griffin.service`, inspect logs via `journalctl -u griffin -f`, and test `/healthz`.
