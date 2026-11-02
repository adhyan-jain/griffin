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
|         +---> exec: agy -p <prompt> --output-format json ...          |
|         |                                                   |
|         v                                                   |
|  [Headless DBus Session Bus] (unix:path=/run/user/1001/bus) |
|         |                                                   |
|         v                                                   |
|  [State dir: ~/.gemini/antigravity-cli/ (+ OS keyring)]             |
|                                                             |
+-------------------------------------------------------------+
```

---

## 2. Authentication (documented mechanisms only)

Source: the official Antigravity install documentation (https://antigravity.google/docs/install/),
re-checked during the 2026-11-02 preflight. Anything not listed here is **not documented** and is
deliberately not implemented in Griffin.

### 2.1 Documented
- **Interactive remote login (one time):** run `agy` in the remote terminal; it prints an
  authorization URL; open it on another device, sign in, paste the code back. A saved token profile
  (OS keyring, "Linux Secret Service/dbus") lets later runs authenticate silently.
- **Unattended alternatives (documented as suitable for headless/CI):**
  `GEMINI_API_KEY` with `~/.gemini/antigravity-cli/settings.json` `{"modelProvider":"gemini"}`
  (per-token billing), or Application Default Credentials (`AGY_ADC_AUTH=true`).

### 2.2 Not documented / unverified (treat as risks, test on the instance)
- Where OAuth tokens are stored on disk and whether they survive reboot without an unlocked keyring.
- Refresh-token lifetime and any re-verification for cloud IP ranges.
- Any non-interactive OAuth login. **There is no `agy auth login`, `--no-browser`, `--format` or
  `--stream`** in `agy --help` (v1.2.0); earlier revisions of this file described them and an
  SSM-seeded `auth.json` "refresh token". Those were unsupported and have been removed.

### 2.3 Procedure
See `docs/DEPLOYMENT_RUNBOOK.md` section 8: authenticate once over SSM as `griffin`, then run the
reboot and stop/start persistence tests *before* enabling `griffin-worker`. If the login does not
survive a reboot, fall back to the documented `GEMINI_API_KEY` mode (accepting per-token cost).

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
sudo -u griffin -i agy -p "Reply with the JSON {\"status\":\"ready\"}" --output-format json
```

### Inspect Worker Logs
```bash
journalctl -u griffin-worker -f
```
