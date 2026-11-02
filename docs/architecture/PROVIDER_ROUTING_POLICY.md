# Griffin Provider Routing Policy & Priority Hierarchy

## 1. Core Routing Principles

1. **Antigravity-First Economy**: The platform is built to maximize value from the user's existing **Google Antigravity subscription**. Antigravity CLI (`agy`) is the **PRIMARY DEFAULT** execution engine for JJ and all specialist workers.
2. **Zero Mandatory Paid API Keys**: Direct model API keys (Google GenAI, Anthropic API, OpenAI) are **OPTIONAL** for Griffin V1. Griffin must function fully out-of-the-box using the authenticated Antigravity CLI on EC2.
3. **No Silent Fallback to Paid APIs**: Griffin will **NEVER** silently route tasks to billed API keys upon Antigravity failure. If Antigravity requires re-authentication, tasks pause and notify the user rather than spending API dollars.
4. **Asynchronous Specialization for Claude Cowork**: Claude Cowork is utilized for deep, long-form document synthesis via Google Drive handoffs when the desktop environment is active.

---

## 2. Provider Hierarchy & Dispatch Logic

```mermaid
flowchart TD
    TaskIn[Incoming Task Specification] --> CheckProviderPref{Task Provider Preference?}
    
    CheckProviderPref -->|Specialist Document Task| CoworkRoute[Route to Claude Cowork Inbox]
    CheckProviderPref -->|Default / Routine Task| CheckAGYHealth{Antigravity CLI Healthy?}
    
    CheckAGYHealth -->|Yes (Authenticated)| DispatchAGY[Execute via Antigravity CLI on EC2<br/>(Subscription - Zero Token Fee)]
    
    CheckAGYHealth -->|No (NEEDS_REAUTH / Error)| CheckPaidFallback{Task Configured with<br/>allow_paid_fallback == true?}
    
    CheckPaidFallback -->|No (Default)| BlockTask[Pause Task: BLOCKED_AWAITING_PROVIDER<br/>Emit High-Priority Alert to User]
    CheckPaidFallback -->|Yes (Explicit User Consent)| CheckAPIBudget{Within Monthly API Budget?}
    
    CheckAPIBudget -->|Yes| DispatchAPI[Execute via Direct Model API<br/>(Track exact token spend)]
    CheckAPIBudget -->|No| BlockTask
```

---

## 3. Provider Routing Matrix by Domain Manager

| Manager Domain | Primary Execution Provider | Execution Mechanism | Secondary / Specialist Provider |
| :--- | :--- | :--- | :--- |
| **JJ (Executive Orchestrator)** | **Antigravity CLI** | Fast headless / streaming CLI (`gemini-3.8-flash-high`) | None (Core Orchestrator) |
| **Career Manager** | **Antigravity CLI** | Structured headless execution (`--json-schema`) | Claude Cowork (Deep Resume Tailoring) |
| **Research Manager** | **Antigravity CLI** | Web search / paper extraction | Claude Cowork (Literature Review Memos) |
| **Engineering Manager** | **Antigravity CLI** | Headless CLI / `claude-code` runner | Direct Claude Code CLI |
| **Hackathon Manager** | **Antigravity CLI** | Opportunity scanning & idea briefs | None |
| **Freelance Manager** | **Antigravity CLI** | Lead parsing & outreach drafting | Claude Cowork (Client SOWs & Proposals) |
| **Networking Manager** | **Antigravity CLI** | CRM updates & message drafting | None |
| **Tech Intelligence Manager** | **Antigravity CLI** | Scheduled arXiv & GitHub sweeps | None |
| **Learning Manager** | **Antigravity CLI** | Curriculum planning & question sets | None |
| **Personal Operations** | **Antigravity CLI** | Daily calendar & email briefings | None |
| **Finance & Benefits** | **Antigravity CLI** | Credit audits & subscription checks | None |
| **Product & Ideas** | **Antigravity CLI** | Idea capture & initial research | Claude Cowork (Comprehensive PRDs) |

---

## 4. Policy Guardrails: Fallback & Cost Protection

### 1. The `allow_paid_fallback` Guardrail
Every task row in the PostgreSQL `tasks` table includes the column:
```sql
allow_paid_fallback BOOLEAN NOT NULL DEFAULT FALSE
```
- **When `FALSE` (Default)**: If Antigravity CLI is unresponsive, times out, or reports session expiration, the task immediately transitions to `BLOCKED_AWAITING_PROVIDER`. The task engine queues the item, and JJ dispatches an alert to the user.
- **When `TRUE`**: Only explicitly designated, mission-critical tasks (e.g. an urgent job application deadline ending within 2 hours) may opt into paid direct API fallback.

### 2. Provider Budget Hard Caps
If the user chooses to supply optional direct API keys for emergency fallbacks:
- A monthly hard ceiling (default: **$10.00 USD**) is configured in `settings.json`.
- When token spend across all fallback runs in `agent_runs` exceeds $10.00, fallback execution is locked out entirely, and all tasks revert to subscription-only execution.
