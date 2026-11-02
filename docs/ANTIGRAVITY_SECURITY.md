# Griffin Antigravity Security & Isolation Controls

## 1. Security Philosophy

Griffin's execution model treats AI subprocesses as untrusted execution environments that must be rigorously constrained. The Antigravity executor operates with strict defense-in-depth protections across five critical domains:

1. **Filesystem Isolation & Path Traversal Defense**
2. **Subprocess Argument & Shell Injection Defense**
3. **Process Group Containment & Resource Exhaustion Defense**
4. **Credential Isolation & Redaction**
5. **No-Silent-Fallback Policy (Zero Paid API Exposure)**

---

## 2. Workspace Isolation & Permissions Model

Every execution attempt is allocated an ephemeral scratch workspace managed by [`internal/provider/antigravity/workspace.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/workspace.go):

```
<WorkspaceRoot>/task-<task_uuid>-<execution_short_id>/
├── task_input.json  (0600 permissions, owner read/write only)
└── [generated artifacts and temporary execution files]
```

### Directory Permissions (`0700`)
- Workspaces are created with `0700` (`rwx------`) mode permissions, strictly preventing access by other local users on the operating system.
- Input payloads (`task_input.json`) are written with `0600` (`rw-------`) mode permissions.

### Path Traversal Defense
To prevent malicious tasks from manipulating directory structures or escaping the designated scratchpad:
```go
cleanTarget := filepath.Clean(targetPath)
expectedPrefix := m.rootDir + string(filepath.Separator)
if !strings.HasPrefix(cleanTarget, expectedPrefix) {
    return "", fmt.Errorf("%w: path %s escapes root %s", ErrWorkspacePathTraversal, cleanTarget, m.rootDir)
}
```
Any attempt to traverse parent directories (`../`), access system roots (`/etc`, `/root`), or escape the designated `WORKSPACE_ROOT` is intercepted and immediately rejected with `ErrWorkspacePathTraversal`.

### Lifecycle Cleanup & Failure Retention
- By default, workspaces and all child contents are immediately and recursively removed upon task completion (`os.RemoveAll`).
- If `RETAIN_WORKSPACES_ON_FAILURE=true` is enabled, failed workspaces are preserved in place for forensic operator analysis, while successful workspaces are purged.

---

## 3. Subprocess Execution & Shell Injection Defense

Griffin forbids passing task goals, prompts, or JSON payloads through shell wrappers (`bash -c`, `sh -c`, `system()`, or shell string concatenations).

### Pure `argv` Slice Execution
All arguments are constructed as discrete string tokens passed directly to `exec.Command(binaryPath, args...)`:
```go
args := []string{
    "-p", prompt,
    "--output-format", "json",
    "--disable-slash-commands",
}
```
Because the operating system executes the binary directly via `execve` syscalls without invoking a shell parser, shell metacharacters (`;`, `&&`, `|`, `` ` ``, `$()`, `>`, `<`) within user prompts or schema payloads are treated strictly as inert data strings, completely neutralizing shell injection vectors.

---

## 4. Process Group Containment & Zombie Prevention

Unmanaged subprocesses risk running runaway loops, spawning background daemons, or becoming zombie processes that exhaust OS file descriptors and PIDs.

- **Process Group Separation**: Every CLI invocation runs inside a fresh process group (`cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`).
- **Hard Timeout**: Enforced via `context.WithTimeout(ctx, cfg.ExecutionTimeout)`.
- **Nuclear Kill Sequence**: If an execution deadline expires, the executor issues `SIGTERM` to `-pgid`, waits a 5-second graceful window, and enforces `SIGKILL` to `-pgid` if processes refuse to exit.
- **Zombie Reaping**: `cmd.Wait()` is always called to release operating system kernel process table entries.

---

## 5. Credential Isolation & Operator-Only Auth

Griffin's architecture strictly isolates authentication tokens:

- **Local Subscription Credentials**: The Antigravity CLI authenticates via the user's interactive subscription and caches OAuth credentials in `~/.gemini/` under the host user account.
- **No Token Ingestion**: Griffin does not read, parse, or store the user's Google OAuth refresh tokens or session keys in application memory, environment variables, or PostgreSQL database tables.
- **Log Redaction**: Subprocess logs and error messages are filtered before being stored in PostgreSQL `task_events` or emitted via REST APIs.

---

## 6. Strict No-Silent-Fallback Policy

A core architectural commitment of Griffin is to **never incur unexpected billing charges**:

1. **Immediate Failure on Expired Auth**: When Antigravity credentials expire or require interactive re-login, the executor immediately identifies the failure and raises `ErrAuthenticationRequired`.
2. **Automated Retries Disabled**: The task is marked as permanently failed (`RetryCount` is not incremented; `retryable: false`). This prevents the worker from cycling into infinite retry loops.
3. **Audit Ledger Notification**: An `AUTHENTICATION_REQUIRED` event is permanently recorded in the task's audit ledger in PostgreSQL.
4. **No Paid API Fallback**: The system **never** attempts silent fallback to paid Gemini API keys, Cloud Billing accounts, or alternate third-party paid providers without explicit, interactive human intervention.
