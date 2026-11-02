# Manager Framework & Standard Specialist Registry

## 1. Overview & Architecture

Griffin structures agent intelligence hierarchically:
- **JJ** orchestrates high-level goals and distributes work.
- **Managers** represent long-lived domain leaders possessing deep context, verification criteria, and capability boundaries.
- **Workers** represent ephemeral task executors instantiated with domain-specific tool access and system prompts.

The **Manager Framework** (`internal/manager/`) provides:
1. Standardized manager definitions with explicit responsibilities and allowed capabilities.
2. A thread-safe, immutable in-memory `Registry` enforcing strict governance.
3. Deliverable verification rules (`VerifyDeliverable`) to evaluate worker deliverables before aggregating final results.
4. Prevention of unauthorized, hallucinated, or rogue manager creation.

---

## 2. The 11 Standard Specialist Managers

Griffin ships with 11 pre-configured standard domain managers:

| # | Manager ID | Name | Responsibilities | Core Capabilities |
| :--- | :--- | :--- | :--- | :--- |
| 1 | `career` | Career Manager | Resume tailoring, job market monitoring, application tracking, interview prep. | `job:apply`, `email:draft`, `web:read`, `research:read` |
| 2 | `research` | Research Manager | Deep technical dives, academic literature analysis, competitive intelligence, synthesis. | `research:read`, `web:read` |
| 3 | `engineering` | Engineering Manager | Architecture review, code implementation, bug diagnosis, test writing, repository maintenance. | `repo:read`, `repo:write`, `research:read` |
| 4 | `hackathon` | Hackathon Manager | Ideation, rapid prototype planning, demo scripting, pitch deck drafting. | `research:read`, `web:read`, `repo:write` |
| 5 | `freelance` | Freelance Manager | Proposal formulation, contract deliverables, milestone tracking, client communication. | `email:draft`, `email:send`, `financial:action`, `research:read` |
| 6 | `networking` | Networking Manager | Professional outreach, CRM tracking, follow-up scheduling, relationship maintenance. | `email:draft`, `email:send`, `calendar:read`, `calendar:write` |
| 7 | `tech_intelligence` | Tech Intelligence Manager | Ecosystem tracking, release notes auditing, AI model benchmarking, security advisories. | `web:read`, `research:read` |
| 8 | `learning` | Learning Manager | Curriculum design, tutorial generation, skill acquisition roadmaps, resource curation. | `research:read`, `web:read` |
| 9 | `personal_ops` | Personal Operations Manager | Schedule coordination, daily briefing synthesis, calendar audits, routine automation. | `calendar:read`, `calendar:write`, `email:draft` |
| 10 | `finance` | Finance Manager | Expense categorization, budget variance analysis, financial reporting, subscription audits. | `financial:action`, `research:read` |
| 11 | `product_ideas` | Product Ideas Manager | Problem statement validation, PRD generation, market sizing, feature prioritization. | `research:read`, `web:read` |

---

## 3. Manager Contract & Deliverable Verification

Every manager implements the `Manager` interface (`internal/manager/manager.go`):

```go
type Manager interface {
    Definition() ManagerDefinition
    InterpretRequest(ctx context.Context, text string) (taskType string, priority domain.TaskPriority, err error)
    VerifyDeliverable(ctx context.Context, task *domain.Task, output []byte) (verified bool, reason string, err error)
}
```

### Deliverable Verification (`VerifyDeliverable`)
When a child worker completes execution, its output payload is inspected by the manager before JJ accepts the deliverable:
- **Empty Output Rejection**: Empty or `{}` outputs fail verification immediately.
- **Structural Validation**: JSON outputs are inspected for required result markers or status fields.
- **Engineering Deliverables**: Validates diff syntax or code block presence.
- **Research Deliverables**: Checks for summary synthesis and source attribution.
- **Audit Logging**: Any failed deliverable emits a structured warning log in the orchestration audit trail.

---

## 4. Governance & Anti-Rogue Protection

To prevent LLM hallucinations from introducing unauthorized manager personas:
1. `Registry.Register(m Manager)` verifies whether `m.Definition().ID` matches one of the 11 recognized domains.
2. If an unauthorized ID is submitted, the registry returns `ErrUnauthorizedManagerRole`.
3. Plan validation (`domain.ValidatePlan`) validates `plan.ManagerID` against the set of registered manager IDs, immediately rejecting rogue plans.

---

## 5. REST API Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/api/v1/managers` | Lists all 11 registered managers with descriptions, allowed capabilities, and verification criteria. |
| `GET` | `/api/v1/managers/{id}` | Retrieves definition and metadata for a specific manager domain (returns 404 for unknown). |
