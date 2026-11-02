# Griffin Cloud Deployment Architecture (Antigravity Primary on EC2)

## 1. Cloud-First Deployment Topology

Griffin is architected to operate permanently in the cloud, completely detached from the user's laptop state. 

Under this architecture, Griffin's core compute runs on an **AWS EC2 Linux VM in `ap-south-1` (Mumbai)**. The VM hosts both the **Griffin Go Monolith** and the **authenticated Antigravity CLI (`agy`)**, operating under a dedicated `griffin` system user. Supabase manages PostgreSQL, Auth, and Storage, while Vercel serves the Next.js PWA frontend.

```mermaid
flowchart TD
    subgraph ClientDevices ["Client Devices"]
        Mobile[Mobile PWA Client]
        Laptop[Desktop Browser PWA]
    end

    subgraph VercelEdge ["Frontend Hosting: Vercel Edge"]
        NextApp[Next.js 15 PWA App]
    end

    subgraph AWSCloud ["AWS Cloud (ap-south-1 Mumbai)"]
        subgraph VPC ["Default VPC (ap-south-1a)"]
            subgraph SecurityGroup ["Security Group: griffin-backend-sg"]
                direction TB
                SGDesc["Inbound: Port 443/80 (or Zero Inbound via Cloudflare Tunnel)<br/>Port 22 SSH Strictly DENIED"]
            end
            
            subgraph EC2Instance ["EC2: t4g.medium (Ubuntu 24.04 LTS ARM64)"]
                SSMAgent[AWS SSM Agent<br/>Remote Session Manager]
                ReverseProxy[Caddy v2 / Cloudflare Tunnel]
                GoCore[Go 1.23 Monolith Service<br/>systemd: griffin.service]
                TaskScheduler[In-Process Cron<br/>Asia/Kolkata]
                
                subgraph AGYEnvironment ["Dedicated 'griffin' User Space"]
                    AGYPool[Antigravity Worker Pool<br/>(Max 2 Concurrent Processes)]
                    Keyring[Encrypted Token Store & Keyring<br/>/home/griffin/.gemini/]
                end
                
                ReverseProxy --> GoCore
                GoCore --- TaskScheduler
                GoCore <-->|Subprocess stream-json / Unix Sock| AGYPool
                AGYPool <--> Keyring
            end
        end

        IAMRole[IAM Instance Profile<br/>AmazonSSMManagedInstanceCore<br/>+ SSM Parameter Store]
        IAMRole --- EC2Instance
        EBS[(30 GB gp3 Volume<br/>Encrypted KMS aws/ebs)] --- EC2Instance
        DLM[Amazon Data Lifecycle Manager<br/>Daily Snapshots - 7 Day Retention] --- EBS
        AWSBudgets[AWS Budgets & Cost Anomaly Alerts<br/>Threshold: $30/mo]
    end

    subgraph SupabaseCloud ["Persistence & Auth: Supabase (ap-south-1 Mumbai)"]
        PG[(PostgreSQL 16 Engine)]
        AuthService[Supabase Auth / GoTrue]
        S3Bucket[Supabase Storage S3 Bucket]
    end

    subgraph ExternalCloudServices ["External Cloud Services"]
        GoogleAI[Google Antigravity Subscription Harness]
        GDriveAPI[Google Drive API]
        GitHubAPI[GitHub REST API]
        ResendMail[Resend Email API]
        Cowork[Claude Cowork via Drive Inbox/Outbox]
    end

    Mobile & Laptop <-->|HTTPS| NextApp
    NextApp <-->|HTTPS REST / SSE| ReverseProxy
    NextApp <-->|Auth Tokens| AuthService

    GoCore <-->|Port 5432| PG
    GoCore <--> S3Bucket
    AGYPool <-->|Outbound HTTPS| GoogleAI
    GoCore <--> GDriveAPI
    GoCore <--> GitHubAPI
    GoCore <--> ResendMail
    Cowork <--> GDriveAPI
```

---

## 2. Workload & Process Layout on EC2

The EC2 instance coordinates two primary software subsystems:

### 1. Griffin Go Monolith (`griffin-core`)
- Managed by `systemd` (`griffin.service`).
- Hosts the REST/SSE gateway on `localhost:8080`.
- Task Engine polls PostgreSQL `tasks` table (`SELECT ... FOR UPDATE SKIP LOCKED`).
- In-process Cron scheduler (`Asia/Kolkata`) initiates daily briefings and scheduled sweeps.
- Enforces queue concurrency backpressure (max 2 parallel Antigravity workers).

### 2. Antigravity Cloud Runner (`agy`)
- Authenticated interactively via the official **Remote SSH OAuth Flow** using AWS SSM Session Manager.
- Executes headlessly using `--output-format json` or `--input-format stream-json --output-format stream-json`.
- Uses `--add-dir` to sandbox file operations to `/var/griffin/workspaces/<task_id>`.
- Token profiles persist on the KMS-encrypted EBS volume.

---

## 3. Cost Architecture (Promotional Credits vs Long-Term)

| Component | Sizing & Service | Pre-Credit Cost | During Credit Period (Cash Outlay) | Post-Credit Operating Cost (Self-Funded) |
| :--- | :--- | :---: | :---: | :---: |
| **AWS Compute** | `t4g.medium` (Credits) $\rightarrow$ `t4g.small` (Post) | $16.35 / mo | **$0.00** (Covered) | $8.18 / mo |
| **AWS Storage** | 30 GB `gp3` encrypted | $2.40 / mo | **$0.00** (Covered) | $2.00 / mo |
| **Public IPv4** | $0 via Cloudflare Tunnel (or $3.65 EIP) | $0.00 – $3.65 | **$0.00** (Covered) | $0.00 – $3.65 / mo |
| **EBS Snapshots** | Amazon DLM Daily Snapshots | $0.50 / mo | **$0.00** (Covered) | $0.50 / mo |
| **SSM Session Mgr** | Zero-cost remote administration | $0.00 | **$0.00** | $0.00 |
| **Supabase Postgres** | Free Tier (or Pro $25/mo) | $0.00 – $25.00 | $0.00 – $25.00 | $0.00 – $25.00 |
| **Vercel Frontend** | Next.js PWA on Vercel Hobby Tier | $0.00 | $0.00 | $0.00 |
| **AI Model API Fees** | **Antigravity Subscription (Primary)** | **$0.00** (Subscription) | **$0.00** (Subscription) | **$0.00** (Subscription) |
| **Total Monthly Cash**| | **$19.25 – $47.90** | **$0.00 – $25.00** | **$10.68 – $39.33** |

*Note: Model execution is 100% subscription-backed via Antigravity CLI, completely eliminating per-token API billing during standard operations.*
