# Capability & Approval Policies Specification

## 1. Overview & Security Model

Griffin operates on a strict **least-privilege, zero-trust capability model** (`internal/domain/capability.go`).

Agents and workers are not permitted unbounded access to external systems. Instead:
1. Every operational power is defined as a strongly typed capability token.
2. Specialist worker definitions and manager profiles declare an explicit whitelist of allowed capabilities.
3. Capabilities categorized as **sensitive** require explicit human approval before execution can proceed.
4. If an agent attempts an action outside its declared capabilities, or executes a sensitive action without human approval, execution is blocked at the engine layer.

---

## 2. Centralized Capability Taxonomy

Griffin defines 11 explicit capabilities:

| Capability Token | Scope | Classification | Description |
| :--- | :--- | :--- | :--- |
| `research:read` | Read-only | **Autonomous** | Reading local documents, research archives, and project files. |
| `repo:read` | Read-only | **Autonomous** | Reading repository source code, git trees, and file contents. |
| `web:read` | Read-only | **Autonomous** | Fetching public web pages, documentation, and search results. |
| `email:draft` | Read-write | **Autonomous** | Drafting emails and saving them as drafts in email outboxes. |
| `calendar:read` | Read-only | **Autonomous** | Reading calendar events, free/busy status, and scheduling metadata. |
| `repo:write` | Mutating | **SENSITIVE** | Writing or modifying code, creating pull requests, committing changes. |
| `email:send` | External | **SENSITIVE** | Transmitting outbound emails directly to external recipients. |
| `calendar:write` | External | **Autonomous / Gated** | Creating or editing calendar invites and appointments. |
| `financial:action` | External | **SENSITIVE** | Initiating financial transfers, payments, or subscription modifications. |
| `job:apply` | External | **SENSITIVE** | Submitting job applications or interview confirmations on user behalf. |
| `infra:destructive`| System | **SENSITIVE** | Terminating cloud instances, dropping database tables, or modifying firewalls. |

---

## 3. Sensitive Capability Detection (`domain.RequiresApproval`)

The engine provides deterministic evaluation of capability risk:
```go
func RequiresApproval(cap Capability) bool {
    switch cap {
    case CapRepoWrite, CapEmailSend, CapFinancialAction, CapJobApply, CapInfraDestructive:
        return true
    default:
        return false
    }
}
```

During plan validation (`domain.ValidatePlan`), any child task requesting a sensitive capability is automatically flagged with `approval_required: true`. If a plan requests a sensitive capability without setting `ApprovalRequired = true`, the plan is rejected as invalid (`domain.PlanStatusRejected`).

---

## 4. Human Approval Workflow & Task Lifecycle

When a task requires human approval:

```mermaid
sequenceDiagram
    participant JJ as JJ Orchestrator
    participant Engine as TaskEngine
    participant DB as PostgreSQL
    participant User as Human User

    JJ->>Engine: Create child task (Sensitive capability)
    Engine->>DB: INSERT task (status='QUEUED' or 'WAITING_FOR_DEPENDENCY')
    JJ->>Engine: RequestApproval(taskID, approval)
    Engine->>DB: UPDATE task status='AWAITING_APPROVAL'
    Engine->>DB: INSERT approvals (status='PENDING', action_payload)
    Note over JJ,DB: Task is halted; workers will not claim it
    User->>Engine: RespondApproval(approvalID, approved=true)
    Engine->>DB: UPDATE approvals status='APPROVED'
    alt Has uncompleted dependencies
        Engine->>DB: UPDATE task status='WAITING_FOR_DEPENDENCY'
    else All dependencies completed
        Engine->>DB: UPDATE task status='QUEUED'
    end
    Note over Engine,DB: Task unblocked for worker claim
```

### Dependency-Aware Unblocking
When an approval is granted:
- If the task has upstream prerequisites that have not yet completed, the task transitions to `WAITING_FOR_DEPENDENCY`.
- If all upstream prerequisites are completed, the task transitions immediately to `QUEUED`, making it claimable by workers.
- If rejected by the human, the task transitions to `CANCELLED`, cascading cancellation to any downstream child tasks.

---

## 5. REST API Integration

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/api/v1/approvals?status=PENDING` | Lists pending approval requests with risk level, action type, summary, and action payload. |
| `POST` | `/api/v1/approvals/{id}/respond` | User submits decision (`{"approved": true/false, "reason": "..."}`). Resumes or cancels the task. |
