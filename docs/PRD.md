# Griffin PRD

**Owner:** Adhyan Jain · **Status:** living document · **Last updated:** 2026-11-06
The only maintained docs are this PRD, `decisions.md` (why each decision was made) and `evolution.md` (how the project changed). `README.md` (the repo's front page) and `agents.md` (instructions for coding agents) stay as repo conventions. Everything else that used to live in `docs/` is folded in here or recoverable from git history.

---

## 1. Product

**Griffin is one person's AI workforce, run from a phone.** You tell JJ (the orchestrator) what you want in plain language. JJ answers directly when it can, or splits the work into steps, gives each step to the right specialist and engine, asks you before anything irreversible, and returns a readable result, a file you keep, or a list that keeps growing.

**The bar:** ask once from your phone and it gets done, fast, including work that needs a browser, your accounts and your mobile apps, with you in control of anything that acts in your name.

**Goals**
1. **Fast:** acknowledgement under 1 s, a simple answer under 15 s, visible progress for anything longer.
2. **Capable:** research with sources, act in a browser, use your accounts, operate your mobile apps.
3. **Safe:** nothing irreversible without your approval; no secret in a prompt, log, result or chat.
4. **Readable:** every result is a clean document; long-lived work is kept as files and lists you own.
5. **Remembers how:** a repeated task type is faster the second time (playbooks).
6. **Cheap:** scale to zero when idle; no paid-API fallback.
7. **Listens:** it does what the owner says. It advises only when it matters and never refuses the owner's own words or wishes.
8. **Scales itself:** every task uses as many sub-agents and tools as it needs, in parallel.

**Non-goals for now:** multiple users or teams, a public product, phone calls on your behalf, payments above a set limit, anything that risks your accounts under a platform's terms without your explicit opt-in.

---

## 2. Owner's standing rules

- Commits are authored by the owner only: no Claude co-author or "generated with" lines, ever.
- Investigate before editing; no fabricated logs or results; never claim success before it is verified live.
- No credentials in reports, chats, logs or prompts.
- No paid API fallback. Claude is not a production provider until the owner signs it in and enables it.
- No broad IAM changes, no extra infrastructure, no additional EC2 instances; keep scale-to-zero.
- If blocked by permissions, say exactly where.
- Docs: maintain only this PRD, `decisions.md` and `evolution.md`.
- Frontend: product grade, phone first, cute and minimal, nothing overlapping or off-screen at 320-412 px; use the design skills and existing libraries (shadcn/ui, Radix, Tailwind).
- **The assistant listens.** The owner is the authority. Griffin follows the owner's instructions, gives advice only when it is genuinely useful, and does not veto the content of the owner's own messages or wishes. Where a safety check exists it is a *confirmation the owner controls* (on or off per action class and contact), not a refusal.
- **Trust follows the source, not the model.** Instructions from the owner are never second-guessed. Text that Griffin *reads from outside* (a web page, an email, someone's message, a file) is untrusted data: it can inform an answer but can never, on its own, trigger an action. This is what keeps full access safe.

---

## 3. Architecture (as built)

| Part | Technology | Where |
|---|---|---|
| Web app (PWA) | Next.js 16, React 19, Tailwind v4, shadcn/ui, React Three Fiber, GSAP, Lenis | Vercel, `https://griffin-pwa.vercel.app`, auto-deploys from `master` |
| API | Go, Lambda (arm64) behind API Gateway HTTP API | `griffin-api`, ap-south-1 |
| Database and auth | Supabase Postgres with row-level security; Supabase Auth (JWKS) | pooler connections; `griffin_app` (runtime), `griffin_system` (system) roles |
| Files | S3 bucket, per-user key prefix | `griffin-artifacts-123456789012-ap-south-1` |
| Worker ("runner") | Same Go binary in worker mode, systemd service, runs Google Antigravity CLI `agy` 1.5.2 | One EC2 instance `i-0123456789abcdef0`, scale-to-zero (wakes on demand, stops after 5 idle min) |

**Flow:** the app calls the API; the API stores tasks and plans and wakes the runner; the worker leases a task, runs `agy` in an isolated workspace, stores the result, and reconciles the plan; the app polls (adaptively) for progress.

---

## 4. What is already built

Status as of 2026-11-06 14:20 UTC, after the last ship.

### 4.1 Live in production
- **Sign-in:** Supabase email login; an email allow-list; CORS allows only the canonical origin (other Vercel hosts redirect to it).
- **Tasks:** create; list with All / Active / Approval / Done; Done filtered by Today, week, month, 2 or 3 months, all time; search; detail page (Overview, Activity, Attempts, Files, Subtasks); cancel; resume a failed or cancelled task; run again; delete a whole finished task tree; selection mode with bulk Cancel, Resume and Delete.
- **JJ:** Ask JJ chat; plans split into steps across 11 managers (career, research, engineering, hackathon, freelance, networking, tech intelligence, learning, personal ops, finance, product ideas) with dependencies and approvals.
- **No duplicate runs:** an identical request that is still running is refused (also when submitted simultaneously); a second tap on Run again is ignored.
- **Plans self-heal:** reading the task list finishes any plan whose steps are all done.
- **Results:** a reader card with formatted Markdown, copy, download, raw view, fold for long answers, and **Save to list**; raw JSON never scrolls sideways.
- **Lists:** living Markdown documents (e.g. an internship tracker): create, add entry, edit, download, delete; any result can be appended as a dated section.
- **Repeat notice:** typing a goal finished in the last 24 h links to that result before you pay for a rerun.
- **Other screens:** Files, Approvals, Workforce, Settings (theme; visual effects Full / Lite / Off; runner; system status with evidence per component).
- **Runner:** wakes on submit and from the Wake button; stops itself when idle; status pill.
- **Polling:** adaptive by task state (5 s running, 20 s waiting, nothing when finished), paused in hidden tabs, identical requests shared.
- **Look and feel:** ink-and-brass design system, Syne display type with Geist; a live WebGL deep field behind every screen; an obsidian JJ core with molten veins as the home hero (running tasks glow, finished ones fade); frosted glass cards; kinetic headlines, count-up numbers, route reveals, magnetic buttons, a cursor ring, inertial scroll and a once-per-session boot animation; all of it respects reduced motion and the effects switch, and touch devices get no pointer effects.

### 4.2 Built and committed, not yet shipped
- **One-step fast path:** plain asks run as one step instead of gather-then-synthesize.
- **File viewer in the app:** every file (results, lists, playbooks, uploads) opens in the app: Markdown formatted, JSON and text readable, images and PDFs inline, anything else a download button; reachable from Files and from a task's Files tab (previously files could only be downloaded).
- **Empty answers say so:** a run that comes back with nothing shows "No information was found for this", and the agent prompts ask it to say so in a sentence instead of returning nothing.
- **Old tasks stay lean:** finished tasks older than 7 days lose their activity and attempt history, and a long answer is shortened to ~600 characters *only when the full answer is saved as a file*; title, goal, status and playbooks stay. Runs on the worker at start and every 6 hours (`TASK_COMPACT_AFTER_DAYS`, 0 turns it off). The task page tells you when an answer was shortened.
- **Phone fixes:** the select-all action bar fits at 320-412 px (it pushed Delete off screen at 360 and below); task tab counts sit inline on one line; Activity, Attempts and Subtasks checked for overlap at four phone widths (none); the UI harness now checks the bar at 320, 360 and 390 px.
- **Listen and voices:** a Listen button on results reads them aloud sentence by sentence with the phone's own voices (stops when you leave the page); Settings has a Voice section with a picker, five speeds and a Preview button, saved per device.
- **Tabs by kind:** a plan shows Overview / Plan / Activity; a worker task shows Result / Activity / Attempts / Files.
- **Managers are brains, enforced:** a test fails if the manager package ever imports anything that can run a provider, start a process, use the network or touch files (checked: it catches a violation).
- **Login screen** restyled to match the app; **deploy scripts** no longer print the Lambda environment; **docs** cleaned up to the PRD, `decisions.md` and `evolution.md`; credential-rotation runbook.

### 4.3 Built, but waiting for the worker to be replaced
The production worker binary is from **2 Nov**. Everything below runs in the worker, so it is not active until the worker is replaced (`infra/ops/deploy-backend.sh worker`):
- Finishing a plan the moment its last step completes (today only startup and task-list reads do it).
- Saving every finished answer as a file (and the trimming of old tasks above).
- Not counting running plans as work in the idle check (the old build can keep the runner awake).
- Provider routing, tool tiers and playbooks (below).

### 4.4 Built, switched off
- **Provider routing:** each step is planned for `agy` (touches the world) or Claude (pure language work). Claude runs through the signed-in CLI only, never an API key, and is off (`CLAUDE_ENABLED=false`).
- **Tool tiers:** `none` (today: text only), `read` (research with sources), `act` (acts for you, always after your approval of that exact task). Off (`AGY_TOOLS_ENABLED=false`). Permission-bypass flags are refused at startup.
- **Playbooks:** a tool-using run leaves a "how I did it" note; the next similar task starts from it; anything that looks like a credential is never saved. Inactive until tools are on.

### 4.5 Quality checks that exist
- Go unit and integration tests against a real Postgres (the full suite passes; one rare setup timeout fixed).
- Web: type check, lint, 111 unit tests, a bundle scan for secrets and dev-only code.
- `web/scripts/ui-tasks.mjs`: 30 UI checks against a mocked API.
- `web/scripts/e2e-real.mjs`: 21 checks driving the real app against the real server, database and worker loop.
- `web/scripts/measure-polling.mjs`: request-rate scenarios.

---

## 5. Why it still feels slow and limited

| What you feel | Cause |
|---|---|
| Slow answers | Each step cold-starts `agy` in a fresh workspace: 12-16 s for short steps, 99-111 s and ~100k tokens for research. Only one `agy` runs at a time, so steps queue. Simple asks were split into two steps (gather, then synthesize); the fast path fixes this once shipped. If the runner is asleep, the EC2 boot comes first. |
| Can't do complex tasks | `agy` is text-only by design today ("You have NO tools"), after earlier hangs and fake completions. It answers from memory: no browsing, logging in, forms or apps. Tool tiers are built but off and unvalidated. |
| "It said it has no WhatsApp access and wouldn't send the message" | Two separate causes. (1) **No tools:** the agent is text-only, so it truly cannot reach WhatsApp. (2) **The hosted model's own policy** declined the wording. Griffin cannot change a vendor's refusals; it can route drafting to a model that does not refuse (7.8). |
| Tasks stuck in Queued | The runner sometimes did not wake; the cause is unknown because the API's logs are not readable with current permissions. |
| Plans stuck in Running | The stale worker could not finish plans; a restart cleared the six stuck ones, the worker still needs replacing. |

---

## 6. Everything the owner asked for, and where it stands

| Ask | Status |
|---|---|
| Fix the priority error on task creation | Done, live |
| Product-grade redesign; then "out of the world" 3D, Active Theory / anime.js level, not just a 3D asset | Done, live (deep field, JJ core, motion system); real-phone feel not yet confirmed |
| Fix the CORS failure | Done, live (canonical origin) |
| Stop the request storm / polling cost | Done, live (adaptive polling) |
| Tasks should never be marked done with no result | Done (live in API; worker side needs the worker replaced) |
| JJ chat must respond | Done, live |
| Delete, resume, run again for finished and other tasks | Done, live |
| Filter finished tasks by this week, month, 2 months, etc. | Done, live |
| Remove the stray mini scrollbar; fix the runner chip | Done, live |
| Monitor and check things myself | Done where permissions allow (read-only worker and API checks); Lambda logs are not readable |
| Never add Claude as a co-author | Followed in every commit |
| A better way to view results | Done, live (reader card) |
| Tasks were repeating | Done, live (duplicate guard and tap lock) |
| Do a thorough end-to-end check with Playwright | Done (`e2e-real.mjs`, 21 checks) |
| Optimise speed and response quality | Partly: duplicate guard, recent-result notice, effort setting, one-step fast path (not shipped); streaming, warm window and parallel steps not built (Phase 1) |
| Cache repetitive tasks ("WhatsApp is slow the first time, quick after") | Playbooks built, off until tools are on |
| A new terminal on EC2 for each task | Each task already runs in its own isolated workspace and process; a live per-task view is not built (Phase 2) |
| Some parts of a task to Claude, others to agy; keep Claude off for now | Done, off |
| Keep files for tasks where info should be stored, e.g. a list when applying to internships | Lists and Save to list live; tasks writing to lists on their own is Phase 3 |
| Product-grade UI/UX everywhere | Every page reviewed at phone and desktop size; login fixed; real-device check pending |
| Give agy tools, with approvals | Built and tested, off; validation is Phase 2 |
| Delete all tasks in the database | Owner does it from the app (Tasks, Select all, Delete); the assistant has no session or database access |
| Access my mobile apps | Planned (sections 7.4 and 9, Phase 4); not built |
| View saved files in the app, no placeholders, say so when there is no information, trim old finished tasks | Built, not shipped (4.2) |
| Phone access, use my own Claude, no guardrails, a self-hosted model | Answered in 7.8 and 7.9; decisions in section 11 |
| Every task uses as many sub-agents as it needs | Designed (7.10); the worker already runs plan steps in parallel up to its capacity; not built beyond that |
| Access all my apps, with permissions the owner sets; learn an app once and reuse it | Designed (7.4, 7.11, 7.12); not built |
| Tools through the phone, web search and curl-style requests | Designed (7.11); not built |
| Managers only plan, assign and summarize; workers do all labor (even summaries); a terminal per worker or manager | Designed (7.13); managers already only route and verify, the rest is plan 2.12 and 2.13 |
| Text to speech: speak results, Siri-like speed and voice, pick a voice and preview it, anime voices | Designed (7.14, plan 1.9 and 2.14); anime voices as a bring-your-own slot only |
| The agent calls people from my phone, several at once, approvals in chat | Designed (7.14, plan 4.4 to 4.6, 3.8); several at once on one SIM is not possible as parallel calls |
| Laptop as a server for agy and Claude Code when it is on; EC2 for agy when it is off; Claude from the phone | Designed (7.15, plan 2.15, 2.16, 5.1, 5.5); the phone part is experimental |
| Keep my resume context and role resume drafts in S3 | Designed (7.15, plan 3.9); upload from Files works today |
| The voice agent can run on the EC2 itself | Yes for speech output and input; not for a large language model (7.8, 7.14) |
| Check Activity and Subtasks don't overlap; cute and minimal | Checked at four phone widths: no overlap; counts made inline (built, not shipped) |
| Select-all action bar was off screen on my phone | Fixed and covered by a test (built, not shipped) |
| The assistant must not deny what I ask for | Principle added (section 2) and design in 7.8; the refusal you hit came from a hosted model, so a no-refusal drafting engine is planned (Phase 2) |
| Only maintain decisions, evolution and the PRD | Done (other Markdown docs removed; recoverable from git) |

---

## 7. Requirements still to build

P0 blocks the product, P1 makes it good, P2 makes it great.

### 7.1 Speed
- **P0 Fast path:** a question that needs no tools or plan is answered in one step. **Built, not shipped:** the planner only splits research into gather-then-synthesize when the request asks for sourced research; everything else is one step. *Done when* "Plan a weekly status report" returns in < 15 s on a warm runner.
- **P0 Streaming:** the answer appears while it is written. *Done when* the first words show within 3 s of the agent starting.
- **P0 Warm window:** the runner stays up for ~15 min after activity instead of booting per task; still stops when idle.
- **P1 Parallel steps:** independent steps run together (raise the agent concurrency to 2; size the instance for more).
- **P1 Effort tiers:** low reasoning effort for simple steps, higher for hard ones.
- **P1 Session reuse:** follow-ups in a thread reuse the warm agent conversation.

### 7.2 Doing real work
- **P0 Validate the read tier** on the production worker: three research tasks return cited sources and change nothing.
- **P1 Live per-task view:** watch a running task step by step (pages, commands, progress), built on the agent's streaming output.
- **P1 Browser automation:** a per-task, isolated browser profile on the worker, screenshots of key steps attached to the task.
- **P1 Credentials vault:** logins in the cloud parameter store, fetched only by the worker at use time, never shown to the model as text where avoidable (stored sessions instead), never logged.
- **P1 Profile:** your resume, links, preferences and standard application answers, editable in Settings, used only by tasks that need them.
- **P1 Tasks write to lists themselves:** each internship application appends a row (company, role, link, status, date).
- **P2 Code sandbox** for data tasks.

### 7.3 Accounts
- **P1 Gmail and Calendar** through official connectors: read and draft freely, send only with approval.
- **P1 Job boards and LinkedIn** through the browser: read first; applications through `act` with approval.

### 7.4 Mobile apps
Use the official API or the web version first; drive the real app only when there is no other way.

| Layer | How | Covers | Limits |
|---|---|---|---|
| 1. Official APIs | OAuth connectors | Gmail, Calendar, Drive, GitHub, Slack, Notion | Most reliable; per-service setup |
| 2. Web versions | Logged-in browser session on the worker | WhatsApp Web, LinkedIn, job boards | Sessions expire; some sites block automation; site terms apply |
| 3. Android app control | An Android device or emulator reachable only from the worker (ADB over a secure tunnel), driven by UI automation (UIAutomator/Appium) with screenshots and the accessibility tree | Any Android app, including ones with no web version | Needs a spare phone, your phone in developer mode, or a cloud emulator; slower; breaks when app UIs change |
| 4. iOS | Shortcuts and the share sheet send things to JJ and show results | Triggering Griffin from an iPhone | iOS does not let a third party control other apps; full iOS app control is not possible |

- **P1** Layer 2 for WhatsApp Web: read and summarise; replies drafted; each send approved.
- **P2** Layer 3 for one chosen Android app first; every action is an `act` step with screenshots before and after.
- **P2** Layer 4: an iOS Shortcut "Send to JJ".
- **All layers:** approval per outward action (send, submit, buy, delete); allow-lists of accounts and contacts; a one-tap "pause all automation"; an audit log with screenshots.

### 7.5 Safety
- **P0** Every action has a risk level. By default high and critical ones *ask* first, and the question shows exactly what will happen. The owner can change any action class or contact to *auto* or *off* (7.8); a confirmation is the owner's setting, never a veto.
- **P0** Text read from outside (pages, emails, messages, files) can never trigger an action by itself (7.8).
- **P0** Rotate the credentials exposed on 2026-11-06 (section 8.3).
- **P1** Spending caps, quiet hours, a one-tap pause-all switch, and an audit log. Allow-lists are the owner's way of granting more trust, not a restriction imposed on the owner.

### 7.6 Reliability and operations
- **P0** Replace the worker; add the worker step to `ship.sh`.
- **P0** Version check: the worker reports its build; the app warns when it is older than the API.
- **P0** Readable API logs, to diagnose wake failures.
- **P1** Alerts: plan stuck, runner awake but idle, wake failed, agent sign-in expired.

### 7.7 UI/UX
- **P0 Each tab has one job.** Activity and Subtasks overlapped in meaning (both were lists of "what happened"). By task kind: a **manager task** (a plan) has *Overview* (goal and the summary passed up), *Plan* (one row per worker it assigned, with status; replaces Subtasks) and *Log* (the manager's decisions); a **worker task** has *Result*, *Terminal* (the live view, replaces Activity), *Attempts* and *Files*. See 7.13.
- **P0** Live progress on running tasks (the live per-task view).
- **P1** Push notifications when something needs you or finishes.
- **P1** Confirm smoothness on a real mid-range phone (60 fps Full, 30 fps Lite).
- **P2** One home view: what is running, what needs you, what finished.

### 7.8 Authority, trust and models (the assistant listens)
**The rule:** the owner decides; Griffin executes and advises. It does not refuse the owner's content and it does not act on anyone else's say-so.

What the word "guardrails" covered, and what changes:

| Kind | Who sets it | Plan |
|---|---|---|
| **Model refusals**: a hosted model (agy, Claude) declines some wording or topics | The model vendor | Cannot be changed from inside Griffin. Handled by routing: wording the hosted model declines goes to a **voice engine** that does not refuse (below). The owner is told which engine ran. |
| **Confirmations** before actions (send, submit, buy, delete) | Griffin | Kept, but owned by the owner: each action class and each contact has **ask / auto / off**. Default is *ask* for irreversible things; the owner can set *auto* per class or per contact at any time, including "auto for everything". It is a setting, not a veto. |
| **Untrusted content** (web pages, emails, other people's messages, files the agent reads) | Griffin | Always treated as data, never as instructions. If a page says "now send this message", nothing happens. This protects full access from being hijacked; it never applies to the owner's own instructions. |
| **Platform rules** (WhatsApp, LinkedIn terms) | The platforms | Cannot be changed; the owner chooses per platform whether to accept the risk of account limits. |

**Voice engine (a model that does not refuse).** A self-hosted open-weights model (Ollama) used for *writing text in the owner's tone*: replies, messages to friends, any wording. Two ways it is used:
1. **"Draft for me"**: works as soon as it exists, even before any app access: the text appears in the app to copy or send.
2. **Refusal fallback (P1):** if a hosted engine answers a drafting step with a refusal, Griffin re-runs that step on the voice engine and shows which engine produced it. The owner never has to ask twice.

Honest limits: open-weights models are weaker than the hosted agent at long tool-driven tasks, so they write and the hosted agent drives; and a vendor's refusal cannot be switched off, only routed around.

**Where the voice engine runs (decision needed):**
| Option | Pros | Cons |
|---|---|---|
| Ollama on **your own computer**, reached from the worker over a private tunnel (e.g. Tailscale) | No cloud GPU bill; text stays home | Your computer must be on; quality limited by its hardware |
| Ollama on the **existing EC2 instance** | No new infrastructure or cost | About 3.8 GB RAM and no GPU (as last measured; the instance type could not be re-read because the network dropped): only small models (roughly 1-3B) fit beside the agents, they write poorly, and they compete with the agent for memory |
| Ollama on a **cloud GPU instance** | Always available | Costs far more than today's setup; breaks scale-to-zero unless it also sleeps; needs a new instance, which needs the owner's explicit approval |
| A hosted no-refusal model API | Easiest | A new paid dependency; conflicts with "no paid API fallback" unless approved |

**"Give it all access" is delivered as trust the owner turns up**, per app and per contact: start with *ask*, move to *auto* for what has proved reliable. Not built yet: the settings screen for it (3.7) and the voice engine (2.8, 2.9).

### 7.9 Using it from your phone
- **Front end:** `https://griffin-pwa.vercel.app`. It is an installable PWA: open it in Chrome (Android) or Safari (iPhone) and choose **Add to Home Screen**; it then opens full screen like an app. Sign in with your Supabase account.
- **"Use my Claude from my phone":** the Claude app on your phone cannot be driven by Griffin, and a phone cannot lend its logged-in session to a server. What *is* possible: sign the Claude CLI in **once on the worker** (device login with your own account, no API key), after which Griffin uses that login for language steps. That is a credential living on the worker (as sensitive as a password; encrypted disk, no remote access except the platform's session manager). Check the Claude terms for automated use of a consumer subscription before relying on it.
- **Your apps from the phone:** see section 7.4: Layer 2 (web versions) and Layer 3 (an Android bridge) are how Griffin operates apps; iOS only supports Shortcuts into Griffin.
- **Credentials on the worker are unavoidable** for anything that logs in for you; the design choice is to store them in the cloud parameter store (encrypted) or as saved browser sessions, never in prompts, files or logs.

### 7.10 Sub-agents: as many as the task needs
- **What exists:** a plan is already a set of steps with dependencies; independent steps can run on separate workers, up to the agent concurrency cap (2 today, on a ~3.8 GB instance).
- **P1 Fan-out planning:** JJ decides per task how many parallel sub-agents help (research five sources at once, check ten job boards, draft several replies), not a fixed two steps. Each sub-agent gets one narrow goal, its own isolated workspace and its own tool tier.
- **P1 Join step:** a synthesizer combines the sub-agents' results into one answer, with a visible list of who did what.
- **P1 Capacity-aware:** fan-out is bounded by what the runner can hold; extra sub-agents queue rather than overload it. Raising capacity (a larger instance, or a second) is a cost decision for the owner.
- **P2 agy's own sub-agents:** `agy` has agent and sub-agent features (`--agent`, `agy agents`). How they behave under our isolation is unknown; capture one real run first (step 1.4) before deciding whether to use them or keep orchestrating from Griffin.
- **Done when:** a "compare five products" task runs five sub-agents at once and finishes in about the time of one.

### 7.11 Tools: web search, HTTP requests, browser, phone
- **P1 Web search** and **HTTP fetch (curl-style)** as first-class read-tier tools, with sources recorded on the task.
- **P0 for the HTTP tool: network safety.** The worker runs on a cloud instance, so the fetch tool must refuse private and link-local addresses (including the instance metadata service at 169.254.169.254, which can hand out the instance's own cloud credentials), refuse non-HTTP schemes, cap response size and time, and follow redirects only to allowed destinations. Without this, a malicious page could make the agent leak the machine's credentials.
- **P1 Browser** (headless Chromium, per-task profile, screenshots).
- **P2 Phone as a tool:** the Android bridge (7.4) and the iOS Shortcut "Send to JJ" give the agent hands on the phone; every action there is logged with screenshots.
- **Permissions per app and per tool**, set by the owner (ask / auto / off), shown in Settings.

### 7.12 App skills: learn an app once, reuse it
The first time Griffin does something in an app it has to work out how: where to tap, what the page looks like, what breaks. Whatever worked should not be rediscovered.
- **P1 Skill documents:** a successful run in an app writes a short, ordered "how to" for that app and action (for example, WhatsApp Web: open a chat, find a contact, send a message). Skills are the same files as playbooks, keyed by app and action, visible in Files.
- **P1 Exploration then replay:** the first attempt explores (with screenshots); once it succeeds, the steps are saved; later tasks start from the skill and only re-explore when a step no longer matches.
- **P1 Staleness:** when a saved step fails, the skill is marked stale, re-explored, and updated in place; apps change their screens, and a stale skill must not be trusted blindly.
- **P1 Safety:** skills never contain credentials, contact lists or message content (the credential filter in ADR-064 applies); they describe *how*, never *what was said*.
- **P2 Sharing a skill across tasks:** a skill learned for one task type is offered to related ones (keyword overlap today, semantic matching later).
- **Done when:** the second "send a WhatsApp message" is measurably faster than the first and makes no exploratory steps.

### 7.13 Agent hierarchy: JJ, managers (brains), workers (hands)
Three layers, each with one job.

| Layer | Does | Never does |
|---|---|---|
| **JJ** (top manager) | Understands the ask, picks which sub-managers it needs, receives their summaries, talks to the owner | Any labor, tool use, or writing of results itself |
| **Sub-manager** (the 11 domain managers) | **Plans** its slice, **assigns** tasks to workers, tracks them, asks a worker to **summarize**, and passes the summary up to JJ | Any labor. It is only a brain: no tools, no executor, no summarizing itself |
| **Worker** | Does one narrow job (research, write, **summarize**, call, tap, fetch) in its own isolated workspace and terminal, and reports the result | Planning or assigning work |

- **Everything laborious is a worker task**, including summaries and JJ's final answer (a `summarize` step whose input is the sub-managers' summaries).
- **Enforced by code, not convention:** manager code gets no executor and no tools, only the ability to create and read tasks; a test fails if a manager path runs a provider.
- **Messages are task records** (assignment, result, summary), so the whole chain is auditable and replayable.
- **Terminals:** every worker has its own terminal, a live view of what it is doing (its isolated process and workspace already exist today). Every sub-manager has its own log, a terminal of decisions: its plan, assignments, status and the summary it sent up.
- **What exists today:** the 11 managers already only route, interpret and verify; JJ's planner assigns steps to workers; chat replies already run on a worker. What is left: JJ's final answer is stitched together in code (`buildFinalResponse`), not by a worker; there is no per-manager log; the live terminal is not built.
- **Cost of the rule:** a worker-run summary adds one agent run (about 13 s warm). Mitigated by skipping it for one-step plans and by reusing a warm conversation (plan 1.7).

### 7.14 Voice: speaking results, and calls
**Speak results (P1).** A **Listen** button on every result, and an optional "read results aloud", that speaks sentence by sentence as text arrives (so it starts at once, like a voice assistant), with pause, stop and speed.
- **Engines:** (a) **device voices** through the browser's speech synthesis: instant, free, many voices, quality varies by phone; (b) **neural voices** (open Piper or Kokoro-class models) generated on the worker, which runs on CPU: more natural, slower to start, audio cached per result. Siri's own voice is not available to web apps; the closest is the best device voice.
- **Voice picker in Settings (P1):** a list of voices, each with a **Preview** button that plays a sample; the choice is remembered per device.
- **Character and anime voices:** imitating a character needs a voice-conversion model, which uses a copyrighted character and a real voice actor's likeness without a licence. Not shipped. The picker offers a **"bring your own voice model"** slot for the owner's personal use; the default catalog is open or licensed voices only. Griffin never clones a real person's voice for calls.
- **Where it runs:** the voice agent can run **on the existing EC2**: neural text-to-speech and speech-to-text (Whisper-class, CPU) are light enough. A *large* no-refusal language model is not: see 7.8. Memory is shared with the agents, so measure before enabling.

**Calls (P2, after the Android bridge).** Griffin places phone calls for the owner, always with approval.
- **Android only:** a call through the phone's dialer and SIM via the Android bridge (7.4). iOS does not allow it.
- **"Many people at once" on one SIM is not possible as independent calls:** a phone holds about one active call plus one on hold, and a conference merges a few parties. Options: (1) **sequential** calls; (2) a **conference** call; (3) a **cloud telephony number** for truly parallel calls (a new paid service; calls come from that number, not the owner's SIM).
- **A live conversation** needs the call audio, which Android restricts. Realistic routes: a telephony provider's media stream, or a speakerphone and microphone loop. Pipeline: speech to text, model, text to speech, under about one second per turn. Hard; scoped after a scripted-call first version (the agent states the message, then the owner joins or the call ends).
- **Legal and consent:** automated or AI-voiced calls and recording are regulated and the rules differ by country (disclosure, consent, do-not-call). The owner is responsible; a setting makes Griffin say it is an assistant when the law requires.
- **Approval for every call:** who, what will be said (the script or goal), maximum length. Approved by one tap or **in chat** (below).

**Approve in chat (P0 for approvals).** Pending approvals appear **in the chat thread** as cards (Approve, Deny, Edit), and typed replies work too: "yes", "no", "approve all the calls to Mom and Dad". Every reply is bound to the specific pending approvals listed in that thread; "approve all" covers only those. Only the owner's signed-in session can approve; text from outside never can (7.8). The Approvals page stays as the full list.

### 7.15 Devices that are ready do the work; the cloud thinks
The EC2 is **not** where tools, logins or apps live: setting them up and authenticating there is slow and puts credentials in the cloud. The hands are the devices that already have everything.

| Machine | Role | Runs |
|---|---|---|
| **Laptop** (Linux) | Browser, local files, Ollama, and the link to the phone | `agy`, Claude Code, Playwright |
| **Android phone** | SIM, native apps, notifications, speech | Controlled from the laptop (ADB, KDE Connect) first; its own host later |
| **EC2 + API** | Queue, planner, approvals, storage, scheduling, text-only backup | `agy` text-only. No tools, no logins, ever |

- **Capabilities, not machines:** each step lists what it *requires* (`browser`, `files`, `phone_apps`, `sms`, `calls`, `messaging`, or nothing for plain text); each online machine advertises what it *can do* by heartbeat; a step is claimed by an online machine that covers it (laptop first for general agent work). If the needed device is off the step waits ("Waiting for your laptop / phone"), then resumes by itself; plain text keeps running on the EC2.
- **Host API:** a small API (heartbeat, claim, events, complete) with a per-host key; hosts never get database credentials. Claim reuses `ClaimNextTask` with one added condition; presence is stored on an `agents` row (no migration).
- **Safety on a personal machine:** each task runs in its own scratch folder that sees only allow-listed folders (default `Documents/Griffin`), with a restricted agent permission mode, full logging and a revocable key per host.
- **Documents:** the laptop host reads `Documents/Griffin` (your `AJ_resume_context` and role resume drafts) and one-way syncs it to your S3 area so the phone and cloud runs can fetch the same files.
- **Claude steps** run on a device with a Claude login; with none on, they fall back to text-only `agy` on the EC2 and say so.

---

## 8. Operations

### 8.1 Ship
- `infra/ops/ship.sh` (dry run) then `infra/ops/ship.sh --apply`: restarts the idle worker, deploys the Lambda, pushes `master` (Vercel deploys the web app), then verifies each live.
- `infra/ops/deploy-backend.sh lambda|worker|all [--apply]`: dry run by default; the worker path checks the binary's sha256, keeps the previous copy and restores it if the health check fails.
- Neither script creates infrastructure, changes IAM or launches instances; neither may ever print configuration (use `--query` to name the fields any AWS call prints).

### 8.2 Run locally
- Test database: a throwaway Postgres container; `GRIFFIN_TEST_DB_URL=postgres://postgres:postgres@127.0.0.1:55440/griffin_test?sslmode=disable go test ./internal/... ./cmd/... ./tests/...`
- Real stack for UI checks: development mode with mock AI and storage, dev login on; the exact commands are in the header of `web/scripts/e2e-real.mjs`.

### 8.3 Credential rotation (owed after the 2026-11-06 leak)
Exposed: the `griffin_app` and `griffin_system` database passwords (Lambda `USER_DATABASE_URL`, `DATABASE_URL`) and `WORKER_API_KEY` (Lambda, worker parameter store, scheduler connection if any). Also exposed earlier: the `postgres` admin password and the publishable key.
1. List consumers by name only (`--query 'Environment.Variables | keys(@)'`; parameter names under `/griffin/prod/`).
2. Generate the new value locally; never paste it anywhere logged.
3. Change it at the source, keeping the old value valid.
4. Update every consumer. The Lambda environment update **replaces the whole map and echoes it**: merge in a private temp file and apply with `--query LastModified --output text`; for parameters, `put-parameter --overwrite --type SecureString --query Version`, then restart the worker.
5. Verify: health check, a signed-in task list, one real task end to end. Only then retire the old value.
6. One secret at a time; never while a task is mid-flight; record the date in `decisions.md`.

### 8.4 IAM
The deploy user has narrow policies (Lambda update and publish; EC2 start/stop and SSM run-command on the one instance; S3 `releases/*`). It cannot read CloudWatch logs, CloudTrail or IAM. Widen only by explicit owner decision.

---

## 9. Plan to finish (approved v4)

Milestones in order; each ends in a demo. "You" marks what only the owner can do. Kill criteria say when to stop and re-scope.

| M | Goal | Work | Proof |
|---|---|---|---|
| **M0 Stabilize** (1-2 d) | Nothing stuck or leaked | Ship the waiting commits (`ship.sh --apply`); replace the worker (`deploy-backend.sh worker`, dry run first); add the worker step to `ship.sh`; worker reports its build and Settings warns when stale; **You:** rotate the leaked credentials, delete old tasks in the app, check the phone, grant read on one log group | Plans finish the instant their last step ends; runner reaches idle shutdown; a stopped runner wakes on submit twice |
| **M1 Fast, listens, simpler** (2-3 d) | Quick wins, no new infrastructure | Worker `IDLE_SHUTDOWN_SECONDS=900`, `MAX_CONCURRENT_ANTIGRAVITY=2`; **done:** Listen + voice picker, tabs by kind, manager-purity test; fast path done; summarize-as-worker-step deferred (below) | Plain-ask p50 before/after; Playwright at 320-412 px |
| **M2 Host protocol + laptop host** (1-2 w) | The hands exist | Host API, `cmd/griffin-host`, capability claim and routing (no EC2 wake while a device can work), waiting states, "ran on" label, live Terminal (after capturing one real `agy` stream) | Host runs a task; stop it and a text step finishes on the EC2 while a device step waits, then resumes. **Stop if** a host needs more than these endpoints |
| **M3 Laptop hands** (1-2 w) | Browser, messaging, approvals, documents | Playwright browser; trust settings (ask/auto per action and contact; outside text can never trigger an action); approve in chat; messaging: click-to-send first, WhatsApp Web second; Ollama drafting + refusal fallback; documents folder + sync; playbooks validated; click-to-call | E2E on a fake chat page; then a real run: read-only, then one approved message to your own number. **Stop sending** on any account warning |
| **M4 Internships** (3-5 d) | The headline journey | Search, shortlist into the tracker list, drafts from your documents, each submission approved in chat, screenshot + tracker row | Dry run on 3 postings, then one real approved application |
| **M5 The phone, through the laptop** (1-2 w) | Phone apps and SMS | Mobilerun/DroidRun over ADB (adopt, don't build); KDE Connect for SMS and notifications (confirm delivery by reading the sent state, not the exit code) | With your phone: list notifications, read a WhatsApp message, one approved SMS to your own number. **Stop if** apps block automation |
| **M6 Calls like companies do** (optional) | Calls without SIM cards | Telephony provider (rented number or SIP trunk) + audio stream + speech pipeline (LiveKit Agents or Pipecat): (1) speak-a-message calls, many recipients in parallel after you approve the list; (2) two-way voice agent; (3) caller ID: rented or your own verified number, never a number you do not own | Needs your approval of a paid provider; India rules (DLT, DND, consent, hours, AI disclosure) checked first. **Skip** if click-to-call is enough |

**Deferred decision (summaries):** the final answer is still stitched in code because every multi-step plan already ends in a worker-written synthesis step; a separate worker summarize step would add about 13 s for little gain. Revisit once live Terminals show whether it matters.

### Every rejected idea and what replaces it
| Rejected | Alternative | When |
|---|---|---|
| Tools, browser, logins or a vault on the EC2 | Devices that already have them; EC2 stays text-only | now |
| SIM calls, many at once; live AI calls | M6: telephony provider and a speech pipeline; concurrency is the provider's (calling ten people is ten API calls) | optional |
| WhatsApp automation risk | Click-to-send (prefilled `wa.me` link) first; WhatsApp Web second; the official Business Platform only for a business number (templates outside 24 h) | M3 |
| A cloud Android bridge | M5: Mobilerun + ADB + KDE Connect from the laptop; a Termux host later only if laptop-off phone control is needed | M5 |
| Neural or anime voices | Phone voices now; free open voices (Piper, Kokoro) on the laptop later; a bring-your-own model slot; no copyrighted characters shipped | later |
| Realtime updates | Adaptive polling; Supabase Realtime later if needed | if needed |
| Web Push | ntfy (one HTTP POST becomes a phone notification; the tap opens the approval chat) | after M3 |
| Gmail and Calendar OAuth | The Gmail and Drive connectors your Claude account already offers, from Claude Code on the laptop | after M3 |
| A profile table | The documents folder is the profile | M3 |
| Sub-agent fan-out machinery | The planner already emits parallel steps; capacity is host concurrency | now |

---

## 10. Metrics

| Metric | Target |
|---|---|
| First visible response | p95 < 1 s |
| Simple answer end to end | p50 < 15 s, p95 < 30 s |
| 3-step plan, warm runner | p50 < 3 min |
| Cold wake to first task claimed | p95 < 60 s |
| Plans stuck with all steps done | 0 (alert after 5 min) |
| Tasks ending with a usable result | > 95% |
| Actions without a required approval | 0 |
| Secrets in logs, prompts, results or chats | 0 |
| Idle runner | stops within 5 min of the queue emptying |

---

## 11. Risks and open decisions

**Risks:** platform terms (WhatsApp, LinkedIn, job boards can restrict accounts that are automated; prefer official APIs and read-only first); iOS cannot be automated beyond Shortcuts; the agent's behaviour with tools is unproven on the production worker; one small runner limits parallel browser and Android work; stored sessions are as sensitive as passwords; warm windows and parallelism cost more.

**Decisions needed from the owner**
1. Phase 0 now: replace the worker and rotate the credentials.
2. Which accounts and apps first (suggested: Gmail, WhatsApp, one job board).
3. Android: a spare phone, your phone in developer mode, or a cloud emulator.
4. A monthly compute ceiling.
5. Whether Claude (signed-in, no API billing) is in scope.
6. Where the voice engine runs: your own computer over a tunnel, a cloud GPU instance, or a hosted no-refusal API.
7. Confirm the trust model in 7.8: your instructions are never second-guessed, outside content can never trigger an action, and each confirmation is ask / auto / off per action class and contact (including auto for everything).
8. A narrow read-only grant for the API's logs (0.5), so failures can be diagnosed.
10. Calls: sequential or conference on your SIM, or a cloud telephony number for parallel calls (a new paid service)? How much live conversation do you want in the first version?
11. Neural voices and speech-to-text on the existing instance: measure memory first, or move to a larger instance?
12. Laptop host: which laptop and OS, which folders it may see, and whether Claude steps should queue or fall back to `agy` while it is off.
13. Documents: upload by hand now, or wait for folder sync (needs the laptop host)?
9. Capacity for sub-agents: stay at 2 agents on the current instance, or approve a larger instance (a cost decision).
