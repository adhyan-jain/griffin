# AWS EC2 Antigravity Authentication Persistence Assessment

## 1. Executive Summary

This document evaluates the persistence, lifecycle, and failure recovery of **Google Antigravity CLI (`agy`)** credentials on a headless **AWS EC2 Linux VM (`ap-south-1`)**.

The fundamental requirement for Griffin is that once authenticated interactively via the supported **Remote SSH OAuth flow**, the cloud runner must operate unattended 24/7 without requiring repeated user sign-ins or keeping a local laptop connected.

### Persistence Assessment Verdict
- **Process Termination Persistence**: **VERIFIED (PASS)**. Tokens persist on disk across independent CLI process runs.
- **Laptop Disconnection Durability**: **VERIFIED (PASS)**. The remote server retains tokens independently of client state.
- **Headless Secret Service & DBus Integration**: **ARCHITECTED & TESTABLE**. Solution specified using `systemd --user` and headless session bus.
- **Token Refresh Longevity**: **ASSESSED / MONITORED**. OAuth access tokens refresh automatically via Google OAuth endpoints; canary probe scheduled for production monitoring.
- **Session Expiry Resilience**: **ARCHITECTED (PASS)**. Strict fail-fast protocol pauses tasks in `BLOCKED_AWAITING_PROVIDER` without financial loss or silent fallback.

---

## 2. Antigravity Credential Storage Architecture

On Linux, the Antigravity CLI manages authentication state using two cooperating mechanisms:

```mermaid
graph TD
    subgraph Host["AWS EC2 Host (Ubuntu 24.04 ARM64)"]
        subgraph GriffinUser["/home/griffin/.gemini/antigravity-cli/"]
            CFG["settings.json (CLI Preferences)"]
            HIST["history.jsonl (Command History)"]
            BRAIN["brain/ (Conversation Logs & Sessions)"]
            TOK["OAuth Refresh & Access Cache (EBS KMS Encrypted)"]
        end
        subgraph Dbus["Headless Session Bus"]
            BUS["/run/user/1001/bus (DBus User Session)"]
            SEC["libsecret / gnome-keyring / File Backend"]
        end
        DAEMON["griffin-backend.service (systemd Daemon)"]
        CLI["agy Subprocess"]
    end

    DAEMON -->|Spawns with DBUS_SESSION_BUS_ADDRESS| CLI
    CLI -->|Reads/Writes Tokens| TOK
    CLI -->|Resolves Keyring Secrets| SEC
    CLI -->|Refreshes Expired Access Tokens| GOOG["Google OAuth2 Token Endpoint (accounts.google.com)"]
```

### 2.1 File Location & Permissions
- **Directory**: `/home/griffin/.gemini/antigravity-cli/`
- **Ownership**: `griffin:griffin`
- **Permissions**: `0700` (`drwx------`) for directories, `0600` (`-rw-------`) for credential caches.
- **At-Rest Encryption**: The underlying AWS EBS volume is encrypted using AWS Key Management Service (KMS). Even if an EBS snapshot or physical drive was detached, token files cannot be decrypted outside the authorized AWS account.

### 2.2 Headless Secret Service Configuration
When running under `systemd` or non-interactive background jobs on Linux, `dbus` user sessions may not automatically spawn unless configured.

To guarantee that `agy` can access keyring services headlessly without an interactive TTY, the following system configuration is implemented:

1. **Enable Systemd User Lingering**:
   ```bash
   sudo loginctl enable-linger griffin
   ```
   *Effect*: Ensures that `systemd --user` and `/run/user/1001/bus` remain active continuously, even when no interactive SSM or SSH session is logged in.

2. **Environment Variable Injection**:
   The Go backend daemon executes with:
   ```ini
   Environment=HOME=/home/griffin
   Environment=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1001/bus
   ```

---

## 3. Token Refresh Lifecycle & Automated Renewal

Antigravity operates on standard OAuth 2.0 with PKCE protocols:

```mermaid
sequenceDiagram
    autonumber
    participant Engine as Griffin Go Engine
    participant CLI as agy Subprocess
    participant Disk as Local Token Cache (/home/griffin)
    participant Google as Google Identity Services

    Engine->>CLI: Exec (agy -p "Task Prompt" --output-format json)
    CLI->>Disk: Read cached Access Token & Expiry
    alt Access Token Valid (< 60 minutes old)
        CLI->>Google: Submit task with valid Bearer Token
        Google-->>CLI: Return LLM inference stream
    else Access Token Expired (> 60 minutes old)
        CLI->>Disk: Read OAuth Refresh Token
        CLI->>Google: POST /o/oauth2/token (grant_type=refresh_token)
        Google-->>CLI: Return new Access Token (+ optional rotated Refresh Token)
        CLI->>Disk: Atomically write updated token cache
        CLI->>Google: Submit task with new Bearer Token
        Google-->>CLI: Return LLM inference stream
    end
    CLI-->>Engine: Emit JSON result
```

### Refresh Token Lifespan
- Under an active Google Antigravity subscription, OAuth refresh tokens do not expire after fixed hours; they persist indefinitely as long as:
  1. The user's Antigravity subscription remains active and in good standing.
  2. The user does not revoke the application grant in Google Account Security settings.
  3. The account password is not changed.
- If Google detects an unexpected IP anomaly (e.g. AWS datacenter IP range), it may occasionally flag a session for re-verification (typically after 30 to 90 days). Griffin provides an automated canary probe to detect this immediately.

---

## 4. Canary Health Probe & Error Detection Protocol

To ensure that Griffin never discovers an expired credential mid-flight during a multi-hour mission, a dedicated background canary probe runs every 6 hours.

### 4.1 Canary Execution Protocol
```bash
# Executed via griffin-canary.timer every 6 hours:
agy -p "ping" --output-format json --print-timeout 15s
```

### 4.2 Health Verification Rules
1. **Status Code `0` & `status: SUCCESS`**:
   - Access token is valid or successfully refreshed.
   - Record `LAST_SUCCESSFUL_CANARY_TS = NOW()` in PostgreSQL table `provider_health`.
2. **Status Code `1` & `authentication required`**:
   - The token cache is missing or the refresh token was invalidated by Google.
   - Trigger the **Immediate Auth Disconnect Protocol**.

---

## 5. Session Expiry & Immediate Recovery Runbook

When an authentication failure is detected (either by the canary probe or during task execution), Griffin executes the following automated response:

```mermaid
flowchart TD
    A["agy returns: 'authentication required'"] --> B["Intercept in Go Provider Adapter"]
    B --> C["Update PostgreSQL: provider_status = BLOCKED"]
    C --> D["Pause Queue: Transition running/pending tasks to BLOCKED_AWAITING_PROVIDER"]
    D --> E["Dispatch Emergency Alert (SSE, Email, Mobile Push)"]
    E --> F["Admin connects via AWS SSM"]
    F --> G["Run: sudo -i -u griffin && agy"]
    G --> H["Authorize via browser & paste code"]
    H --> I["Canary succeeds: provider_status = OPERATIONAL"]
    I --> J["Resume paused tasks automatically"]
```

### Step-by-Step Re-Authentication Runbook

When alerted that Antigravity requires re-authentication:

1. **Connect to EC2 via AWS SSM**:
   ```bash
   aws ssm start-session --target <instance-id> --region ap-south-1
   ```
2. **Switch to Griffin User**:
   ```bash
   sudo -i -u griffin
   ```
3. **Execute Interactive Login**:
   ```bash
   agy
   ```
4. **Complete the Remote OAuth Flow**:
   - Copy the printed URL to your local laptop browser.
   - Sign in and authorize.
   - Paste the authorization code back into the SSM prompt.
5. **Verify Resolution**:
   ```bash
   agy -p "ping" --output-format json
   ```
6. **Unblock Griffin**:
   The Go backend's next canary run (or admin `/resume` command) detects the restored credentials, unblocks `BLOCKED_AWAITING_PROVIDER` tasks, and resumes execution seamlessly. No tasks are lost, and no per-token API charges are incurred.
