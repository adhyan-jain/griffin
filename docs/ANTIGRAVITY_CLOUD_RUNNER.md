# Griffin — Antigravity Cloud Runner & Unattended Authentication

This guide details how Google Antigravity CLI (`agy`) operates unattended on cloud execution runners (such as AWS EC2 scale-to-zero instances) without manual browser login prompts or desktop GUI dependencies.

---

## 1. Execution Architecture

The Griffin worker invokes `agy` headlessly through the Go worker process (`internal/provider/antigravity/`):

```
+-------------------------------------------------------------+
| AWS EC2 Execution Runner (Ubuntu 24.04 ARM64)               |
|                                                             |
|  [griffin systemd worker service]                           |
|         |                                                   |
|         v                                                   |
|  [AntigravityExecutor (Go)]                                 |
|         |                                                   |
|         +---> exec: agy --format json --stream ...          |
|         |                                                   |
|         v                                                   |
|  [Headless DBus Session Bus] (unix:path=/run/user/1001/bus) |
|         |                                                   |
|         v                                                   |
|  [Credential Store: ~/.gemini/antigravity-cli/]             |
|                                                             |
+-------------------------------------------------------------+
```

---

## 2. Unattended Authentication & Token Refresh

When running in headless cloud environments, interactive OAuth redirect flows (`http://localhost:PORT/oauth/callback`) are unavailable. Antigravity uses persistent credential caching:

### 2.1 Credential Location
- **Configuration Directory**: `/home/griffin/.gemini/antigravity-cli/`
- **Session Tokens**: Stored encrypted in the user's local keyring or secret store, accessible via the user DBus daemon.
- **Refresh Flow**: `agy` automatically handles OAuth2 refresh token exchanges in the background before token expiration without requiring browser interaction.

### 2.2 Pre-Seeding Authentication on Cloud Runners

For unattended bootstrap, credentials can be securely transferred to the instance profile or SSM Parameter Store:

1. **Option A (Secure SSM Parameter Store)**:
   Store the initial refresh token in AWS SSM Parameter Store (encrypted with KMS):
   ```bash
   aws ssm put-parameter \
     --name "/griffin/antigravity/refresh_token" \
     --type "SecureString" \
     --value "$AGY_REFRESH_TOKEN" \
     --region ap-south-1 \
     --profile griffin-bootstrap
   ```
   On instance startup, cloud-init retrieves the parameter and writes it into `/home/griffin/.gemini/antigravity-cli/auth.json`.

2. **Option B (One-Time SSM Session Login)**:
   Connect once to the runner using AWS Systems Manager:
   ```bash
   aws ssm start-session --target <INSTANCE_ID> --region ap-south-1 --profile griffin-bootstrap
   sudo -u griffin -i
   agy auth login --no-browser
   ```
   Follow the CLI instructions to authenticate. The resulting tokens persist across EC2 start/stop cycles because the root EBS volume is configured with `DeleteOnTermination: false`.

---

## 3. Systemd User Lingering & DBus Session Bus

Google Antigravity CLI requires an active DBus session bus to interact with the system secret service. In standard Linux environments, user sessions and DBus daemons terminate when an SSH or SSM session disconnects.

To ensure `agy` functions continuously in the background:

### 3.1 Enable User Lingering
```bash
# Executed by infra/user-data.sh during instance setup
loginctl enable-linger griffin
```
Lingering ensures that `systemd --user` starts at boot and remains running even when no interactive sessions exist.

### 3.2 Environment Variables in `/home/griffin/.bashrc` & systemd unit:
```bash
export PATH="/home/griffin/.local/bin:$PATH"
export DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/1001/bus"
```

---

## 4. Headless Execution Contract

The Griffin `AntigravityExecutor` invokes `agy` with strict constraints:

- **JSON Output**: Output is constrained to parseable JSON schema structures.
- **Timeout Management**: Tasks specify strict step timeouts; if `agy` exceeds the lease duration, the process is terminated and the task is safely recovered.
- **Heartbeat Reporting**: While `agy` executes long-running goals, the Griffin worker loop sends periodic heartbeats to `POST /api/v1/workers/{task_id}/heartbeat` to prevent lease expiration.

---

## 5. Troubleshooting & Health Verification

### Check Antigravity CLI Version
```bash
sudo -u griffin -i agy --version
```

### Test Headless Execution
```bash
sudo -u griffin -i agy "Output a JSON object with status 'ready'" --format json
```

### Inspect Worker Logs
```bash
journalctl -u griffin-worker -f
```
