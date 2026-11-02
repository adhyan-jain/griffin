# AWS Infrastructure Cost Estimation & Sizing Model

## 1. Regional Scope & Context

- **Region**: **AWS Asia Pacific (Mumbai) — `ap-south-1`**
- **Architecture**: **ARM64 (AWS Graviton2)**
- **Operating System**: **Ubuntu 24.04 LTS (ARM64)**
- **Workload Profile**: Go 1.23 modular monolith, in-process cron scheduler (`Asia/Kolkata`), PostgreSQL-backed task queue worker pool (5–10 goroutines), Caddy reverse proxy, SSM Agent.
- **Credit Posture**: AWS promotional/free credits available; this model separates the credit burn rate from the post-credit net cash operating cost.

---

## 2. Instance Comparison & Sizing Analysis

| Dimension | `t4g.small` | `t4g.medium` (Recommended for Credits) | `t4g.large` |
| :--- | :---: | :---: | :---: |
| **vCPU** | 2 | 2 | 2 |
| **Memory (RAM)** | 2 GiB | 4 GiB | 8 GiB |
| **Processor** | AWS Graviton2 (64-bit ARM) | AWS Graviton2 (64-bit ARM) | AWS Graviton2 (64-bit ARM) |
| **Baseline CPU Performance** | 20% of 2 vCPUs | 40% of 2 vCPUs | 40% of 2 vCPUs |
| **CPU Credits Earned / hr** | 12 credits/hr | 24 credits/hr | 36 credits/hr |
| **Network Bandwidth** | Up to 5 Gbps | Up to 5 Gbps | Up to 5 Gbps |
| **Hourly On-Demand Rate** | **$0.0112 / hr** | **$0.0224 / hr** | **$0.0448 / hr** |
| **Monthly Compute (730 hrs)**| **$8.18 / month** | **$16.35 / month** | **$32.70 / month** |

### Workload Sizing Evaluation
- **Griffin Go Monolith RSS**: ~80 MB – 150 MB.
- **In-process Scheduler & Worker Pool**: ~100 MB – 250 MB under active multi-task load.
- **Caddy Reverse Proxy**: ~30 MB.
- **OS, systemd, AWS SSM Agent**: ~250 MB – 350 MB.
- **Scratch Disks & File Buffering**: ~200 MB – 500 MB.
- **Total Estimated Active Footprint**: **~660 MB – 1.25 GiB**.

#### Evaluation
1. **`t4g.small` (2 GiB)**: Squeezes comfortably within normal operating limits (~60% memory utilization). However, during parallel web scrapes or code compilation, memory headroom is constrained. Requires configuring a 2 GiB swapfile to guarantee zero OOM risk.
2. **`t4g.medium` (4 GiB) — RECOMMENDED FOR CREDIT PHASE**: Provides 2.7+ GiB of free memory headroom. Zero OOM risk, allows concurrent worker burst, local integration test runs, and caching. Ideal to run while AWS credits are active.
3. **`t4g.large` (8 GiB)**: Unnecessary over-provisioning for a single-user Go application whose heavy LLM inference and database reside on external cloud providers.

---

## 3. Comprehensive Itemized Cost Breakdown

### Monthly Cost by Component (`ap-south-1`)

| Infrastructure Component | Specification / Usage | `t4g.small` | `t4g.medium` | `t4g.large` | Eligible for AWS Credits? |
| :--- | :--- | :---: | :---: | :---: | :---: |
| **EC2 Instance Compute** | 730 hours On-Demand | $8.18 | $16.35 | $32.70 | **YES** |
| **EBS Storage (`gp3`)** | 30 GB root volume ($0.08/GB-mo) | $2.40 | $2.40 | $2.40 | **YES** |
| **EBS IOPS & Throughput** | Baseline 3,000 IOPS & 125 MB/s | $0.00 (Included) | $0.00 (Included) | $0.00 (Included) | Free Baseline |
| **Public IPv4 Address** | 1 In-use public IPv4 ($0.005/hr) | $3.65 | $3.65 | $3.65 | **YES** |
| *(Alternative: Cloudflare Tunnel)*| *(Zero public IP fee)* | *($0.00)* | *($0.00)* | *($0.00)* | *Zero cost* |
| **EBS Snapshots (Backups)** | 10 GB incremental retention ($0.05/GB) | $0.50 | $0.50 | $0.50 | **YES** |
| **Data Transfer OUT** | First 100 GB/month free tier | $0.00 | $0.00 | $0.00 | Free Tier |
| **AWS SSM Session Manager** | Remote shell administration | $0.00 | $0.00 | $0.00 | Free Service |
| **CloudWatch Metrics** | Basic 5-minute metrics | $0.00 | $0.00 | $0.00 | Free Tier |
| **CloudWatch Logs** | Ingestion < 2 GB/mo (First 5 GB free) | $0.00 | $0.00 | $0.00 | Free Tier |
| **AWS Budgets & Alerts** | 2 Action-enabled budgets | $0.00 | $0.00 | $0.00 | Free Tier |
| **Total AWS Monthly Charge (Standard IP)** | | **$14.73** | **$22.90** | **$39.25** | **100% Credit Eligible** |
| **Total AWS Monthly Charge (Cloudflare Tunnel)** | | **$11.08** | **$19.25** | **$35.60** | **100% Credit Eligible** |

---

## 4. Promotional Credits vs. Real Cash Operating Cost

### Phase A: During AWS Promotional Credit Window
During the period where promotional credits apply to your AWS account:
- **Net AWS Cash Outlay**: **$0.00 / month** (Billed against promotional credit balance).
- **External Services**:
  - Supabase Database & Auth: Free Tier ($0.00) or Pro Tier ($25.00/mo).
  - Next.js Frontend on Vercel: Free Hobby Tier ($0.00).
  - Direct Model APIs (Gemini / Anthropic): Usage-based pay-as-you-go ($5.00 – $15.00/mo).
- **Total Net Cash Spend During Credits**: **$5.00 – $40.00 / month**.

### Phase B: Post-Credit Ongoing Operating Cost (Self-Funded)
Once promotional credits expire or are depleted:
- **Optimized Long-Term AWS Footprint**:
  - Downsize instance to **`t4g.small`** ($8.18/mo).
  - Rightsize EBS storage to **25 GB `gp3`** ($2.00/mo).
  - Adopt **Cloudflare Tunnel** for ingress (eliminates $3.65/mo IPv4 charge).
  - Retain automated EBS snapshots ($0.50/mo).
  - **Net AWS Spend**: **~$10.68 / month** (or ~$14.33/mo with a standard public IPv4).
- **Total Net Cash Spend Post-Credits**:
  - AWS Infrastructure: ~$10.68 – $14.33 / mo.
  - Supabase: $0.00 (Free Tier) – $25.00 / mo.
  - Vercel: $0.00 (Hobby).
  - AI Model APIs: $5.00 – $15.00 / mo.
  - **Total Monthly Operating Cost**: **$15.68 – $54.33 / month**.

---

## 5. Cost Guardrails & Automated Budget Alerts

To guarantee that promotional credits are not inadvertently drained or unexpected bills incurred:

1. **AWS Budget**: Set a hard monthly limit of **$30.00 / month**.
   - Alert 1 (Email): Triggered at **50% of budget ($15.00)**.
   - Alert 2 (Email): Triggered at **80% of budget ($24.00)**.
   - Alert 3 (Email + High Priority Notification): Triggered at **100% of budget ($30.00)**.
2. **AWS Cost Anomaly Detection**:
   - Enable an Anomaly Monitor for "All AWS Services".
   - Configure an immediate email notification when an unexpected spend spike > $5.00 is detected.
3. **Credit Balance Monitoring**:
   - The **Finance & Developer Benefits Manager** runs a weekly cron sweep tracking AWS credit expiration dates and remaining balances, surfacing warnings in JJ's morning briefing.
