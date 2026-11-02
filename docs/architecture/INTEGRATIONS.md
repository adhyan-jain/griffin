# Griffin Integrations Architecture & Feasibility

## 1. Integrations Feasibility Matrix

| Integration | Primary Purpose | Authentication Mechanism | Feasibility Classification | Requires Manual User Setup? |
| :--- | :--- | :--- | :--- | :--- |
| **Google Drive** | Document storage, manager folders, Claude Cowork handoff. | Google OAuth 2.0 (Refresh Token) or Google Service Account. | **FEASIBLE NOW** | Yes: One-time GCP Console OAuth App creation. |
| **GitHub** | Repository tracking, automated PR reviews, issue management. | GitHub Fine-Grained Personal Access Token (PAT) or GitHub App. | **FEASIBLE NOW** | Yes: Generate PAT or install GitHub App. |
| **Supabase** | Managed PostgreSQL 16, Auth (GoTrue), Storage buckets. | Service Role Key (Backend only) & Anon Key (Frontend). | **FEASIBLE NOW** | Yes: Supabase project provisioning. |
| **Cloud Hosting (Railway / Render)** | 24/7 backend monolith, scheduler, worker pool. | Platform API Token / Git deployment webhook. | **REQUIRES CLOUD DEPLOYMENT** | Yes: Railway project linking. |
| **Email (Inbound & Outbound)** | Daily briefings, job application alerts, outreach drafting. | Inbound: Webhook via Resend / Postmark. Outbound: SMTP / Resend API. | **FEASIBLE NOW** | Yes: Domain verification (DNS DKIM/SPF). |
| **Google Calendar** | Scheduling checks, meeting prep, daily briefing context. | Google OAuth 2.0 (`https://www.googleapis.com/auth/calendar.readonly`). | **FEASIBLE NOW** | Yes: Consented during Google OAuth flow. |
| **Web Research & Scraping** | arXiv papers, job boards, tech news, company info. | Google GenAI Search Grounding or Tavily API + Jina Reader API. | **FEASIBLE NOW** | Yes: Tavily / Google API key. |
| **Google Antigravity CLI** | Routine local agent tasks & existing Google subscription. | Local session credentials (`~/.gemini/antigravity-cli`). | **FEASIBLE WITH LIMITATIONS** | Yes: Interactive terminal login on host. |
| **Claude Cowork** | Deep document synthesis & specialist tasks. | File-based handoff via Google Drive synced directory. | **REQUIRES ASYNC MANUAL WORKFLOW** | Yes: User installs Claude Desktop and syncs Drive. |

---

## 2. Deep Dive: Google Drive Integration

### Workspace Folder Hierarchy
The integration initializes and maintains a structured directory structure within the user's Google Drive:

```
Google Drive/
└── Griffin/
    ├── Inbox/
    │   └── Claude_Cowork/          # Tasks deposited by cloud backend for Cowork
    ├── Outbox/
    │   └── Claude_Cowork/          # Results written by Cowork, monitored by Griffin
    ├── Managers/
    │   ├── 01_Career/              # Resumes, cover letters, application tracking
    │   ├── 02_Research/            # Literature memos, BibTeX, experiment notes
    │   ├── 03_Engineering/         # System designs, architecture ADRs
    │   ├── 04_Hackathons/          # Project submissions, slide decks
    │   ├── 05_Freelance/           # Proposals, contracts, SOWs
    │   ├── 06_Networking/          # Contact dossiers, outreach drafts
    │   ├── 07_Tech_Intelligence/   # Weekly intelligence digests
    │   ├── 08_Learning/            # Curricula, reading notes, cheat sheets
    │   ├── 09_Personal_Ops/        # Daily agendas, checklists
    │   ├── 10_Finance/             # Credit ledgers, renewal alerts
    │   └── 11_Product_Ideas/       # PRDs, feature specs, concept briefs
    └── Archives/                   # Completed historical projects
```

### Technical Implementation
- **API**: Google Drive API v3.
- **Conversion**: Markdown deliverables produced by workers are uploaded as native Google Docs (`mimeType: application/vnd.google-apps.document`) or standard `.md` files.
- **File Watching**:
  - *Option A (Push)*: Google Drive Webhook Push Notifications (requires a public HTTPS endpoint on Railway).
  - *Option B (Poll)*: Lightweight polling of `Inbox/Claude_Cowork` and `Outbox/Claude_Cowork` changes every 60 seconds using `drive.changes.list` with a persistent `pageToken`.

---

## 3. GitHub Integration

### Scope & Permissions
The **Engineering Manager** connects to GitHub using a GitHub Fine-Grained Personal Access Token (PAT) with restricted scopes:
- `repo` (Issues, Pull Requests, Contents read/write)
- `workflow` (Monitoring GitHub Actions CI/CD runs)

### Worker Automation Scenarios
1. **Automated Issue Breakdown**: When a feature idea is approved, the Engineering Manager creates a GitHub issue with structured acceptance criteria.
2. **Automated PR Review**: Webhook receives PR opening events -> dispatches a code review worker -> writes inline feedback and summary comment on GitHub.
3. **Repository Health Scan**: Weekly cron job checks for stale branches, open vulnerability alerts (Dependabot), and failing CI runs.

---

## 4. Email & Calendar (Personal Operations & Networking)

### Email
- **Outbound**: Routed through **Resend API**. **Strict Rule**: Outbound emails drafted by agents (referral requests, cold outreach, follow-ups) can **NEVER** be sent automatically. They enter the `approvals` table with full HTML preview, recipient, and subject. Only after the user clicks "Approve & Send" does the backend trigger the dispatch.
- **Inbound Triage**: Forwarding address configured to receive alerts or webhook parsing, extracting job application responses (e.g. "Invitation to Interview" or "Update on your application") to automatically transition the application pipeline status.

### Google Calendar
- Read-only access to primary calendar.
- Read during the 08:00 IST morning briefing generation to summarize:
  - Upcoming meetings and preparation notes.
  - Deep work focus blocks.
  - Conflict warnings (overlapping events).

---

## 5. Web Research & Intelligence Scrapers

Griffin uses a dual-layer web retrieval system:
1. **API Grounding**: Primary search queries run through Google GenAI Search Grounding or Tavily Search API, obtaining synthesized summaries with cited URLs.
2. **Clean Markdown Extraction**: When a worker needs to read a full research paper or article, it routes the URL through Jina Reader API (`https://r.jina.ai/<url>`), which returns clean markdown stripped of ads, navbars, and cookie banners.
