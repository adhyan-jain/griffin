# Griffin Architecture Decision Records & Risk Analysis (Antigravity Primary)

## 1. Architectural Decision Records (ADRs)

### ADR-001: Go Modular Monolith over Microservices
- **Status**: ACCEPTED
- **Context**: Griffin coordinates 11 domain managers, an orchestration engine, a task queue, and a scheduler.
- **Decision**: Build a single Modular Monolith in Go 1.23.
- **Consequences**: Eliminates network serialization overhead and distributed transaction complexity; simplifies deployment to a single EC2 host.

---

### ADR-002: PostgreSQL `SKIP LOCKED` over Redis / Celery / RabbitMQ
- **Status**: ACCEPTED
- **Context**: Need a persistent, crash-resilient task queue with leasing and DAG dependencies.
- **Decision**: Implement distributed queue semantics directly in PostgreSQL 16 using `SELECT ... FOR UPDATE SKIP LOCKED`.
- **Consequences**: Single source of truth; zero additional broker infrastructure to maintain.

---

### ADR-003: Server-Sent Events (SSE) over Bidirectional WebSockets
- **Status**: ACCEPTED
- **Context**: Real-time token streaming and task progress to the PWA.
- **Decision**: Standardize on REST for client-to-server mutations and SSE for server-to-client streaming.
- **Consequences**: Native HTTP/2 streaming, built-in reconnection, lower server resource footprint.

---

### ADR-004: Asynchronous Google Drive Inbox/Outbox for Claude Cowork
- **Status**: ACCEPTED
- **Context**: Leverage Claude Cowork for deep reasoning without unsupported desktop automation.
- **Decision**: Implement an asynchronous document-based Inbox/Outbox handoff protocol via Google Drive.
- **Consequences**: Seamlessly exploits Cowork's document capabilities when desktop is active; runs asynchronously without blocking cloud workflows.

---

### ADR-005: Next.js PWA over React Native for V1
- **Status**: ACCEPTED
- **Context**: Unified desktop and mobile access from Day 1.
- **Decision**: Build an installable Progressive Web App (PWA) using Next.js 15, Tailwind CSS, and Web Push.
- **Consequences**: Single codebase for mobile and desktop; instant updates without app store reviews.

---

### ADR-006: In-Process Persistent Cron Scheduler in `Asia/Kolkata`
- **Status**: ACCEPTED
- **Context**: 08:00 IST morning briefings and recurring scans must run reliably in the cloud.
- **Decision**: Run `robfig/cron/v3` inside the Go monolith on EC2, synchronized with PostgreSQL `schedules`.
- **Consequences**: Timezone-aware, durable across restarts, with automatic missed execution catch-up logic on boot.

---

### ADR-007: Stateless Worker Specifications over Long-Lived LLM Sessions
- **Status**: ACCEPTED
- **Context**: Prevent runaway background agent processes and unbounded memory usage.
- **Decision**: Workers are declarative execution specifications spun up per task, executed against a provider driver, and immediately decommissioned.
- **Consequences**: Zero idle memory/CPU usage; deterministic cost; full horizontal scalability.

---

### ADR-008: AWS EC2 Graviton2 (`ap-south-1`) as Primary Cloud Compute
- **Status**: ACCEPTED
- **Context**: Host both the Go monolith and the authenticated Antigravity CLI runner.
- **Decision**: Deploy on **AWS EC2 in `ap-south-1` (Mumbai)**. Utilize **`t4g.medium`** (2 vCPUs, 4 GiB RAM) during promotional credits, with a pathway to **`t4g.small`** post-credits.
- **Consequences**: Dedicated Linux environment allowing persistent CLI execution and KMS-encrypted credential storage.

---

### ADR-009: AWS Systems Manager (SSM) Session Manager over Inbound SSH Port 22
- **Status**: ACCEPTED
- **Context**: Secure remote administration and initial interactive OAuth login.
- **Decision**: **Completely close inbound port 22.** Standardize on **AWS SSM Session Manager** via IAM instance profile.
- **Consequences**: Zero open administrative ports; no SSH keys to manage; full CloudTrail audit logging.

---

### ADR-010: Next.js PWA on Vercel Edge over EC2 Self-Hosting
- **Status**: ACCEPTED
- **Context**: Preserve EC2 VM resources for the Go monolith and Antigravity CLI.
- **Decision**: Host Next.js on **Vercel Edge** (Hobby Tier, Free).
- **Consequences**: Prevents Node.js SSR memory spikes from competing with `agy` processes on the VM.

---

### ADR-011: Antigravity CLI on EC2 as the PRIMARY Execution Engine
- **Status**: ACCEPTED
- **Context**: Maximize the user's active Google Antigravity subscription and avoid unnecessary pay-per-token API costs.
- **Decision**: Make the **authenticated Antigravity CLI (`agy`) on EC2 the PRIMARY default execution provider** for JJ and all specialist workers.
- **Consequences**:
  - *Positive*: Zero per-token API charges; full reasoning power under subscription; native support for headless JSON, streaming, schemas, and conversation resumption.
  - *Negative*: Requires initial interactive OAuth setup via SSM and automated canary monitoring for session longevity.

---

### ADR-012: Concurrency & Queue Backpressure for Antigravity Runner
- **Status**: ACCEPTED
- **Context**: Prevent account-level rate limits and VM memory exhaustion from unbounded parallel CLI processes.
- **Decision**: Enforce a hard concurrency limit of **2 concurrent `agy` subprocesses** managed via a Go semaphore, with queue backpressure handled in PostgreSQL.
- **Consequences**: High stability, zero memory thrashing on `t4g.medium`, guaranteed priority scheduling.

---

### ADR-013: Strict Prohibition of Silent Paid Fallback
- **Status**: ACCEPTED
- **Context**: Prevent unexpected billing when Antigravity CLI experiences session expiration or network timeouts.
- **Decision**: Tasks default to `allow_paid_fallback = false`. If Antigravity is unavailable, tasks pause in `BLOCKED_AWAITING_PROVIDER` and alert the user.
- **Consequences**: Zero unexpected API billing; explicit user control over costs.

---

## 2. Technical Risk Analysis & Mitigations

| Risk | Probability | Severity | Mitigation Strategy |
| :--- | :--- | :--- | :--- |
| **Antigravity CLI OAuth Token Expiration** | Medium | Medium | • Run automated canary check every 6 hours (`agy -p "ping"`).<br>• If token expires, set runner status to `NEEDS_REAUTH` and alert user via Push/Email.<br>• Provide 1-command re-authentication via SSM (`aws ssm start-session` -> `agy`).<br>• Tasks remain queued safely without failing. |
| **Datacenter IP OAuth Challenges** | Low | Medium | Authentication is performed interactively through the official Remote SSH OAuth flow from the VM. Because the user explicitly pastes the authorization code returned by Google, the session is tied to the legitimate user profile. |
| **VM Memory Exhaustion during Multi-Task Runs** | Low | High | • Hard worker pool concurrency cap (2 active CLI processes).<br>• PostgreSQL `SKIP LOCKED` backpressure.<br>• 2 GiB swapfile configured on EC2 instance. |
| **Accidental API Bill Shock** | Very Low | High | • Direct model APIs disabled by default (`allow_paid_fallback = false`).<br>• Hard $10.00 monthly cap if fallback is explicitly enabled. |
