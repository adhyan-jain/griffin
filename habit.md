# Griffin User Behavioral & Lingo Habits Profile

This document records the user's personal linguistic patterns, mobile typing slips, communication shorthands, contact aliases, device postures, and execution expectations. Griffin's orchestrator, classifier, and device hands adapt to these conventions so crude or shorthand commands work seamlessly without requiring explicit system instructions or configuration from the user.

---

## 1. Colloquial Lingo & Contact Aliases

| Shorthand / Slang | Canonical Term | Interpretation & Routing Invariant |
| :--- | :--- | :--- |
| **`bhai`** | Brother / Primary Contact | Default family recipient for personal communication. Treated as a high-priority contact across WhatsApp and phone calling hands. |
| **`mom`**, **`maa`** | Mother | Family contact alias for personal ops. |
| **`dad`**, **`papa`** | Father | Family contact alias for personal ops. |
| **`wassup`**, **`sup`**, **`yo`** | Casual greeting | Colloquial greeting in Indian/urban English context (e.g. *"message bhai wassup"* means send text "wassup" to bhai). |
| **`hellooo`**, **`heyy`** | Casual greeting | Expressive informal greetings. |

---

## 2. Shorthand Verbs & Mobile Typing Slips

Users typing on mobile keyboards frequently produce character transposition and phonetically shortened verbs. Griffin normalizes these transparently:

| Input Pattern | Canonical Action | Channel Default |
| :--- | :--- | :--- |
| **`sned`**, **`snd`** | `send` | Default messaging hand (WhatsApp) |
| **`msg`** | `message` | Default messaging hand (WhatsApp) |
| **`txt`** | `text` | Default messaging hand (WhatsApp) |
| **`ping`**, **`tell`** | `message` / `inform` | Default messaging hand (WhatsApp) |
| **`watsapp`**, **`wapp`**, **`watsp`**, **`whatsap`** | `whatsapp` | WhatsApp Hand (`com.whatsapp` or WhatsApp Web) |
| **`dm`** | `direct message` | Instagram Hand (`com.instagram.android` / IG Web) |
| **`fwd`** | `forward` | Message forwarding |

---

## 3. Implicit Messaging Defaults

- **Implicit Platform**: When the user requests a message to a person without specifying a platform (e.g., *"message bhai wassup"*, *"text bhai I'm running late"*, *"msg mom that dinner is ready"*), Griffin **defaults to WhatsApp**.
- **Deliverable Guard**: If the request contains a deliverable noun (e.g., *"send a report"*, *"send invoice"*), it routes to file synthesis or email, not interpersonal chat.
- **Engineering Guard**: If the request contains programming verbs or software entities (e.g., *"write a whatsapp bot"*, *"send an api call"*), it routes to engineering, never device hands.

---

## 4. Device Posture & Locked Phone Invariant

### Real-World Mobile Condition
In everyday life, users do **not** leave their phones permanently unlocked or awake with the screen on. The phone is typically locked, in a pocket or on a desk, with power on and connected to mobile data or Wi-Fi.

### Execution Handling
1. **Automated Keyguard Wake & Dismissal**:
   - When an Android hand step runs via ADB, Griffin sends `KEYCODE_WAKEUP` followed by `wm dismiss-keyguard` and `input keyevent 82` (KEYCODE_MENU) to dismiss standard swipe lockscreens.
   - If an optional phone PIN is provided via `GRIFFIN_PHONE_PIN`, ADB inputs the PIN headlessly.
2. **Transparent Laptop Web Fallback**:
   - If the phone has a secure biometric/PIN lock and remains locked, ADB returns `ErrNotReady`.
   - `whatsapp.Messenger` immediately and automatically falls back to **WhatsApp Web** on the laptop.
   - WhatsApp Web runs in a headless Chrome browser session that connects to WhatsApp multi-device cloud servers. It does **not** require the phone's screen to be on or unlocked, requiring only that the phone has battery and active internet (mobile data).
   - The user's task succeeds with zero friction.

---

## 5. Sent Message Verification & Read-Back

- **Delivery Confirmation**: Every send action (`MessagingSend`) waits for real-time confirmation:
  - Verifies the composer box held the exact text before send was clicked.
  - Verifies an outgoing message bubble with identical text appears on the right side of the chat.
  - Verifies the message is not stuck in a "pending/clock" state.
- **Read-Back Skill (`MessagingReadRecent`)**:
  - Reading recent chat messages from a recipient confirms sent messages and fetches replies.
  - Griffin combines send + read-back verification in its integration test suites.

---

## 6. Smart Resource Allocation

- **Google Antigravity CLI (`agy`)**:
  - Web research, search queries, company scraping, and daily routine tasks.
  - Fast message formulation (`wa_compose`) and casual text drafting.
- **Claude Pro CLI (`claude`)**:
  - Deep linguistic reasoning, keyword-optimized resume tailoring, and complex code reviews.
- **S3 Vault Document Caching**:
  - Working document files (`.pdf`, `.docx`, `.md`, `.txt`) accessed more than once are automatically encrypted client-side with AES-256-GCM and cached in tenant-isolated S3 Vault.
  - Source code repositories (`.git`, `node_modules`, `vendor`) are strictly excluded.

---

## 7. Phone-First Default & Headless Execution Invariant

- **Default Target Device**: Unless the user explicitly specifies `"laptop"`, `"desktop"`, or `"computer"`, Griffin assumes all personal tasks, device controls (ringtone, wallpaper, developer options, searches), and communications target the user's primary **phone**.
- **Headless Background Execution**: Messaging and system tasks must execute headlessly in the background (using notification quick-reply or background host automation). Griffin avoids forcefully launching apps in the phone foreground to prevent interrupting user activity.
- **Immediate Push Notifications**: Any replies received on tracked job applications, cold emails, or alumni LinkedIn/WhatsApp messages must trigger an immediate high-priority push notification to the user's phone.

---

## 8. Career & Resume Quality Guardrail (Strict Claude Enforcement)

- **Resume Tailoring & Personalization**: All resume alterations, role-tailored edits, and outreach drafting against `~/Downloads/AJ_Resume_Context.md` must strictly be executed using **Claude Pro CLI**.
- **Prohibition of Silent Fallback**: If Claude is unavailable, rate-limited, or throws an error, Griffin **must halt and alert the user immediately**. It is strictly forbidden to fall back to Antigravity CLI or lower-tier models for resume modifications.
- **Mandatory Pre-Application Approval Gate**: Under no circumstances may Griffin auto-submit an internship application, send a LinkedIn cold message, or dispatch a cold email without explicit prior user approval. Every application package (company, role, stipend, tailored resume variant, cover note, questionnaire responses) must pause in `AWAITING_APPROVAL` (`requires_approval = true`, `draft_only = true`), send an interactive approval prompt to the user's phone, and submit only after the user explicitly reviews and confirms.

---

## 9. Freelance & Client Acquisition Playbook

Griffin enforces a disciplined multi-channel outreach engine, client vetting guardrails, and automated disqualification rules to secure high-quality engineering and LLM consulting contracts while avoiding exploitative leads.

### 9.1 Multi-Channel Outreach Matrix

| Channel | Target Audience | Approach Strategy | Primary Proof Link |
| :--- | :--- | :--- | :--- |
| **Hacker News monthly "Who is hiring" / "Seeking freelancer" threads** | Founders hiring directly | Email the address in the post on the same day with exactly one relevant proof link. | `StoryTrace`, `OS-Tutor-RAG`, `Proteus` |
| **Product Hunt & launch platforms** | Teams shipping LLM features this week | Use the product, identify one real technical/UX flaw, and email the founder with a constructive fix. | `OS-Tutor-RAG`, `StoryTrace` |
| **Discord & Slack communities (agent frameworks, dev tools)** | Founders stuck on technical hurdles & fellow engineers | Answer technical questions in public for two weeks before initiating any direct pitch. | `Proteus`, `StoryTrace` |
| **X & LinkedIn contractor requests** | Early-stage startups posting "looking for a contractor" | Reply publicly with a specific technical example or architecture outline, then follow up with a direct message. | `StoryTrace`, `OS-Tutor-RAG` |
| **Wellfound & YC Work at a Startup** | Early startups listing contract or part-time roles | Submit application through the portal, then email the founder directly with a tailored pitch. | Resume + `StoryTrace` / `Proteus` |
| **Small AI & Automation Agencies** | Agency owners with more client pipeline than engineering capacity | Offer to subcontract as a specialist at a fixed rate. | `Proteus`, `OS-Tutor-RAG` |
| **GitHub OSS Repositories** | Engineering teams with open LLM-related issues | Fix one open issue with a clean pull request, then inquire if they need paid contract assistance. | User's merged PRs + `StoryTrace` |
| **Upwork & Contra** | Budget or quick-turnaround clients | Secondary side channel only; maintain strict minimum rates and never prioritize over direct founder outreach. | Portfolio / GitHub profile |
| **Local Business / Supplier & Dealer Contacts** | Local business owners and distributor networks | Schedule introduction and present a live demonstration of `AquaDrop`. | `AquaDrop` (`https://github.com/AdhyanJain/AquaDrop`) |

---

### 9.2 Weekly Routine Target (20 / 2 / 1 Scorecard)

To secure consistent paid contracts within 4 to 8 weeks, Griffin tracks and prompts the following weekly discipline:
- **20 Targeted Outward Pitches / Week**: Personalized messages across HN, founders, and contract posts (average 3-4 per business day).
- **2 Public Community Answers / Day**: High-value technical responses across developer Discords, Slacks, StackOverflow, or GitHub discussions (14/week).
- **1 Fixed GitHub Issue / Week**: One substantive merged pull request or issue resolution on a target company's open-source repository.

---

### 9.3 Walk-Away Red Flags (Strict Automated Disqualification)

Griffin's opportunity validator automatically disqualifies leads exhibiting any of the following anti-patterns:
1. **Deferred Funding Payment**: "We will pay once we raise funding / seed round" (ref. RxGPT experience). Immediate rejection.
2. **Unpaid Test Work > 2 Hours**: Any assessment, trial task, or uncompensated coding project requiring more than 2 hours of labor without compensation.
3. **Equity or Exposure in Place of Money**: Offers substituting equity, revenue share promises, or "portfolio exposure" for guaranteed cash compensation.
4. **Upfront Fees**: Any demand for upfront payment, onboarding fees, training costs, or equipment purchase requirements.
5. **Absence of Written Scope / Scope Creep**: Ambiguous deliverables without written acceptance criteria or milestone definitions.
6. **Credential Access Requests**: Any request for personal account credentials, passwords, or direct access to personal developer profiles.
7. **Platform Escrow Bypass Pressure**: Demands to bypass established platform escrow systems (e.g. Upwork, Contra) prior to the release of initial payment or milestone funding.

---

### 9.4 Green Flags (High-Priority Engagement)

Griffin flags opportunities as high-priority when they meet all three positive indicators:
1. **50% Upfront Deposit**: Client agrees to a 50% deposit before work commences (or funded platform escrow milestone).
2. **Crisp Definition of "Done"**: Scope of work contains clear, unambiguous, testable completion criteria.
3. **Single Named Decision-Maker**: Exactly one person has final authority to sign off on deliverables and authorize payment release.

---

### 9.5 Outward Outreach Approval Gate

All outward client pitches, cold emails, forum messages, and contractor replies generated by Griffin must follow the human approval gate (ADR-110). Griffin prepares the draft with genuine proof links and stages it in `AWAITING_APPROVAL`. No outreach is dispatched autonomously without explicit user confirmation.

---

## 10. Job Applications, Resume Tailoring & Referral Networking Invariants

### 10.1 Project Count & Mutual Exclusion Rules
- **Project Count Limits**: Every tailored resume must contain between 3 and 5 projects. The strict default is **exactly 3 projects**, unless a specific role directly warrants 4 or 5 projects to cover broad requirements.
- **Mutual Exclusion Rule**: `LabTutor` and `OS_RAG` (or `OS-Tutor-RAG`) are conceptually and architecturally too similar (both RAG-driven pedagogical systems). **Never include both in the same resume.** Choose one based on role alignment.

### 10.2 SGLang Open-Source Contribution Highlight
- Adhyan was part of the student team from **ACM VIT Vellore** that contributed to documentation and developer guides for **SGLang** (high-performance LLM serving and structured inference framework).
- When tailoring applications for LLM evaluation, serving infrastructure, or companies like **Sarvam AI**, prioritize and explicitly highlight this SGLang open-source contribution alongside EchoTales, ComputeHive, and Griffin.

### 10.3 Multi-Contact LinkedIn Referral & Networking Engine
For every target job opening, Griffin never relies on a single contact. It automatically constructs and coordinates a **Multi-Contact Referral Cluster of 3 to 5 targeted individuals per company** across strict priority tiers:
- **Priority Tier 1 (Highest Priority - 2 Contacts)**: VIT Vellore alumni working in the same domain/team who were active members of **ACM, ISTE, MFC, or TAM** student chapters/clubs (direct junior/senior rapport and shared club history).
- **Priority Tier 2 (Technical Domain Alumni & Engineers - 1 to 2 Contacts)**: VIT Vellore alumni working in the same technical subdiscipline (backend, ML/AI, distributed systems) or senior engineers on the target team.
- **Priority Tier 3 (Engineering Lead or Founder - 1 Contact)**: Engineering manager, tech lead, or founder overseeing the relevant team.
- **Sequenced & Staggered Dispatch**: Griffin sequences outreach by priority tier (Tier 1 first). It staggers dispatch across 15 to 45 minute intervals (minimum 300s gap + 180s jitter) and limits total outreach to a maximum of 10 to 15 messages per 24 hours to prevent anti-automation detection.
- **Single-Referral Engagement Rule (Pause on Response)**: As soon as any contact at a company responds positively or offers an internal referral link, Griffin transitions remaining targets for that company to `closed` with the note `Referral already secured via [Contact Name]`, immediately halting further cold outreach.
- **Approval Gating**: All referral messages are staged in `AWAITING_APPROVAL` with `ActionType: "REFERRAL_OUTREACH"` (and `status = 'drafted'` in `outreach_targets`). No outreach message is dispatched without human approval.

---

### 10.4 Pre-Flight Application Verification & Proof of Submission Receipts
To eliminate black-box execution failures and ensure 100% reliable submissions:
- **Pre-Flight Validation**: Prior to form interaction, Griffin verifies all required fields (candidate legal name, contact email, phone, GitHub, LinkedIn, portfolio link, 1-page tailored resume PDF, and grounded technical screening answers). Any unknown screening questions abort submission and escalate to human input.
- **Staged Dry-Run Mode**: When dry-run or verification mode is active, Griffin navigates to the portal, populates all form inputs, attaches the tailored resume, highlights all filled fields, and pauses execution before the final submission button, allowing the user to review the live browser session.
- **Timestamped Screenshot Receipts**: For every automated application and staged outreach message, Griffin captures a full-page timestamped visual screenshot saved to `/home/adhyan/Documents/Griffin/reports/receipts/<company>_<action>_<timestamp>.png` and links it in the task audit log as verifiable proof of work.
- **Audit Ledger Tracking**: Every application is recorded in `/home/adhyan/Documents/Griffin/internships/applied.json` with submission timestamp, applied role, portal URL, resume path, and skills offered, preventing accidental re-applications.

---

### 10.5 Anti-Automation & Platform Safety Invariants
- **Daily Rate Limiting**: Maximum 15 LinkedIn messages per rolling 24-hour period.
- **Human Emulation Jitter**: Minimum pause of 300 seconds plus 180 seconds randomized jitter between outgoing communications.
- **Quiet Hours**: Zero automated messaging between 22:00 and 08:00 local IST time.
- **Contextual Hook Diversity**: Every message must feature unique technical angles (e.g. ACM VIT SGLang docs, ComputeHive Redis Lua leasing, EchoTales bitemporal resolution); zero identical template broadcasts.

---

### 10.6 Append-Only Tracker Ledgers & Cumulative History
All Griffin trackers (`INTERNSHIP TRACKER`, `FREELANCE & CLIENT DISCOVERY`, `HACKATHON TRACKER`, `THREAD ALERTS`, `applied.json`, `referral_tracker.json`) must operate under strict **APPEND-ONLY** semantics:
- When a tracker runs on its scheduled cadence, it loads the existing dataset from disk.
- Newly discovered opportunities or posts are deduplicated against existing records using canonical keys (`company`, `role`, `link` / URL).
- New rows are appended to the cumulative dataset. Existing history, past submission records, deadlines, and founder metadata must **never be truncated, replaced, or erased**.
- Output files (`open_tracker.json`, `tracker.csv`, `tracker.md`) represent the living, cumulative archive of all discovered listings.

---

### 10.7 Communication Tone, Age Awareness & Outreach Etiquette
- **Age Awareness**: Adhyan is a 20-year-old 3rd year undergraduate (B.Tech CSE, VIT Vellore). Outreach recipients (staff engineers, engineering managers, directors, founders) are often senior professionals.
- **Polite & Humble Demeanor**: All messages must maintain a warm, polite, and respectful tone. Avoid presumptive or demanding phrasing like "what I offer you" or "what I can do for you."
- **Courteous Inquiries**: Politely inquire if they would be open to reviewing the attached resume or giving brief advice on an internal referral for the summer opening.
- **Resume Attachment**: Every outreach draft must explicitly reference and attach the tailored resume file/link for immediate review.
- **Zero Emojis**: Maintain strictly zero emojis across all drafted pitches, messages, and correspondence.

---

### 10.8 Exam Freeze Window & Cadence Protection
- When the user announces an exam period (e.g., active exams through a specified date), Griffin must record the hold in the database (`scheduled_for`, `input_payload.paused_until`, `input_payload.exam_hold = true`).
- Targeted routines (such as `DSA COACH` paused until November 29 07:30 IST) are frozen in the task queue to prevent unwanted notifications or dispatches during revision and exam hours.
- Human approval cards stay safely staged in `PENDING` with zero automatic expiry, allowing the user to review and approve them when their exams conclude.

---

### 10.9 Mandatory Application Portal Deadlink Verification
- Before Griffin stages or releases LinkedIn referral outreach messages for any target company, it MUST first verify that the company's application portal URL is active (HTTP 200) and that the candidate's application submission package is staged in a ready-to-submit state.
- If a portal link is dead (404, DNS failure), Griffin must resolve it to an active verified careers page (e.g. Wellfound, Workable, company direct) or halt outreach until the verified portal is confirmed.

---

### 10.10 Professional Identity: Applied AI & Systems Builder (Not a QA Tester)
- Adhyan's identity is an **Applied AI engineer focused on evaluation and reliability** ("I build LLM systems where you can check whether the output is right").
- Reliability, failure testing (such as ComputeHive's 1,000-job chaos run), and evaluation harnesses are framed as empirical proof of engineering rigor in building production AI and distributed systems.
- Do NOT emphasize testing or QA in isolation; never position the candidate as a QA tester or test engineer.
- Highlight tangible production metrics and real-world scale: 70 students, 600+ concurrent users, 1,100+ applications, bitemporal knowledge graphs, Redis Lua atomic leasing, and SGLang documentation contributions.
- Mid-sized substantive pitches (100–120 words) with warm, polite, age-conscious demeanor and direct resume attachments.

---

### 10.11 In-Place Approval Deduplication & Stale Card Cleanup
- When follow-up instructions modify an existing campaign, outreach draft, or application pitch, Griffin must update the existing record in `approvals` and `outreach_targets` in place based on recipient handle and action summary.
- Griffin must never create duplicate parallel cards in history.
- Any obsolete or superseded approval requests (e.g., from updated rules or cancelled roles) must be deleted or removed from the database entirely so history does not clutter the user's Approvals screen.

---

### 10.12 Worktree Isolation for Framework & Security Upgrades
- Framework migrations and security codemods (e.g., Next.js security upgrade) must execute in an isolated Git worktree in the background (`griffin-security-upgrade`), ensuring primary codebase development and application pipelines remain unblocked and stable.







