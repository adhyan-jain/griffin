# Griffin Antigravity Cloud Runner Architecture

## 1. Executive Summary & Purpose

Griffin elevates the **Google Antigravity CLI (`agy`)** to the **PRIMARY** execution and reasoning engine of the platform, powered by the user's existing Antigravity subscription. 

Rather than relegating Antigravity to local desktop development or defaulting to pay-per-token model APIs, Griffin deploys the authenticated `agy` CLI on a dedicated, persistent **AWS EC2 Linux VM in `ap-south-1` (Mumbai)**. The Go modular monolith orchestrates, schedules, and monitors task execution, communicating with the local Antigravity runner via a secure, unexposed internal interface.

---

## 2. Core Architectural Topology

```mermaid
flowchart TD
    subgraph EC2 ["AWS EC2 Instance: t4g.medium (ap-south-1 Mumbai)"]
        subgraph UserSpace ["Dedicated Linux User: 'griffin'"]
            direction TB
            GoBackend["Griffin Go Backend Monolith<br/>(Task Engine, Scheduler, API)"]
            UnixSock[("Internal IPC / Unix Domain Socket<br/>/run/griffin/agy.sock")]
            RunnerService["Griffin Antigravity Runner Adapter<br/>(Process Manager & Stream Demuxer)"]
            
            subgraph AGYWorkerPool ["Antigravity Worker Pool (Concurrency Limit: 2)"]
                Worker1["agy CLI Process 1<br/>(stream-json / headless)"]
                Worker2["agy CLI Process 2<br/>(stream-json / headless)"]
            end
            
            Keyring["OS Keyring / Token Cache<br/>/home/griffin/.gemini/antigravity-cli/"]
        end
        
        EBS[(30 GB gp3 Encrypted EBS Volume)]
        SSM["AWS SSM Agent<br/>(Remote Session Manager)"]
    end

    subgraph DataPlane ["Supabase Cloud"]
        Postgres[(PostgreSQL 16 Tasks & Leases)]
    end

    subgraph GoogleAI ["Google AI Infrastructure"]
        AntigravityHarness["Antigravity AI Harness<br/>(Active Subscription Tier)"]
    end

    GoBackend <-->|FOR UPDATE SKIP LOCKED| Postgres
    GoBackend <-->|Unix Domain Socket| UnixSock
    UnixSock <--> RunnerService
    RunnerService --> Worker1 & Worker2
    Worker1 & Worker2 <--> Keyring
    Worker1 & Worker2 <-->|Outbound HTTPS (mTLS/OAuth)| AntigravityHarness
    EBS --- Keyring
    SSM -.->|Interactive SSH-less Login| UserSpace
```

---

## 3. Runner Architecture & Process Isolation

### 1. Dedicated System User (`griffin`)
- The Antigravity CLI and Go backend run strictly under an unprivileged Linux system user: `griffin` (`uid=1001`, `gid=1001`).
- Home directory: `/home/griffin` with permissions `0700` (`rwx------`).
- Configuration and cached tokens: `/home/griffin/.gemini/antigravity-cli/` stored on an AWS KMS-encrypted EBS volume.
- The runner is **never** executed as `root` and is never exposed on an external public IP or port.

### 2. Internal Communication Interface
The Go backend interacts with Antigravity through the internal `AntigravityRunnerAdapter`:
- **Invocation Mode A: Direct Subprocess Exec with `stream-json`**:
  - The Go backend's task worker spawns `exec.CommandContext(ctx, "agy", "--input-format", "stream-json", "--output-format", "stream-json", "--model", spec.Model, ...)` with connected standard I/O pipes.
  - Maintains zero network listener attack surface.
  - Process lifecycle is tied directly to the Go worker context and task lease timeout.
- **Invocation Mode B: Managed Worker Pool (`griffin-agy-daemon`)**:
  - A lightweight local daemon maintains a pre-warmed pool of up to 2 long-lived `agy` processes listening over a local Unix domain socket (`/run/griffin/agy.sock`, permissions `0660 griffin:griffin`).
  - Eliminates process startup overhead for sequential multi-turn JJ conversations.

---

## 4. Key Headless Capabilities Utilized

Verified from official Antigravity CLI documentation (`https://antigravity.google/docs/cli/headless/`) and experimental testing:

1. **Machine-Readable JSON Output**:
   - `--output-format json`: Emits a single complete JSON object containing `conversation_id`, `status` (`SUCCESS` / `ERROR`), `response`, `duration_seconds`, `num_turns`, and detailed `usage` (input, output, thinking, cache read, total tokens).
2. **Real-Time Streaming (`stream-json`)**:
   - `--output-format stream-json`: Emits newline-delimited JSON (NDJSON) events:
     - `init`: Emitted once at startup with `conversation_id`, `cwd`, `tools`, and `permission_mode`.
     - `step_update`: Streaming events with `state` (`ACTIVE` / `DONE`), `step_type` (`agent_response`, `tool`, `user_input`), `text_delta` (incremental token fragments), and `tool_info` (tool call parameters and outputs).
     - `result`: Emitted at turn completion with cumulative stats and parsed structured output.
3. **Structured Schema Enforcement**:
   - `--json-schema '<schema>'`: Directly constrains model responses to a formal JSON schema, populating `result.structured_output` for zero-shot deterministic machine extraction.
4. **Persistent Conversation Continuity**:
   - `--conversation <UUID>`: Reliably resumes any prior conversation by ID across separate CLI process runs, reading historical context from disk transcripts.
5. **Model & Effort Selection**:
   - `--model <slug>`: Explicitly pins models (e.g. `gemini-3.8-flash-high`, `gemini-3.8-flash-medium`, `claude-sonnet-4-6`, `claude-opus-4-6-thinking`). Exits non-zero with `ERROR` status if an invalid model is requested, preventing silent fallback.
   - `--effort <low|medium|high>`: Adjusts reasoning effort dynamically per task priority.
6. **Workspace Sandboxing & Permission Policies**:
   - `--add-dir /var/griffin/workspaces/<task_id>`: Restricts agent file operations to isolated per-task directories.
   - Pre-configured `permissions.allow` rules in `/home/griffin/.gemini/antigravity-cli/settings.json` allow required tools while soft-denying dangerous operations.

---

## 5. Security & Isolation Invariants

1. **Zero Public Network Exposure**: The Antigravity runner has no public HTTP, gRPC, or TCP ports open. All communication is strictly local via standard streams or Unix domain sockets.
2. **Encrypted Storage**: The EBS root and data volume are encrypted with AWS KMS (`aws/ebs`). If the VM is stopped, credential caches on disk are inaccessible without AWS KMS authorization.
3. **Non-Root Execution**: `systemd` sandboxing directives (`ProtectSystem=full`, `ProtectHome=false`, `NoNewPrivileges=true`, `PrivateTmp=true`) constrain the `griffin.service`.
4. **No Credential Exfiltration**: Credentials are generated via the official interactive OAuth loop on the VM and never transmitted over external network endpoints.
