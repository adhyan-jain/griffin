# Griffin PRD

**Owner:** Adhyan Jain · **Status:** living document · **Last updated:** 2026-11-07
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

- **Host protocol and laptop host (M2, ADR-077):** `hosts` table with row-level security; owner endpoints (`POST/GET /api/v1/hosts`, `PATCH/DELETE /api/v1/hosts/{id}`: register, list with mode and waiting steps, pause, change grants, revoke) and host endpoints (`/api/v1/host/heartbeat`, `claim`, `tasks/{id}/heartbeat|complete|fail`) authenticated by a per-host token stored only as a hash. A step may declare `input_payload.requires`; the cloud runner never claims such a step and it does not wake or hold awake the runner; a plain step needs a host with the `agent` capability; the runner is not woken while a live agent host can do the work. `cmd/griffin-host` connects out, keeps presence (every 15 s against a 45 s live window), claims, runs `agy` text-only and reports. Host runs record attempts (start, finish, error), so the Attempts tab shows them. Off until `HOSTS_ENABLED=true`. Verified end to end with the real binaries (12 checks: waits with no host, host takes it, runner takes plain work when the host is off, waiting step resumes, presence lapses, revoked host exits) and under the production RLS roles.

- **Devices page (M2 UI):** a Devices entry in the navigation. Shows which device is primary in plain words (laptop, phone, none), each device with online/paused/offline-since, what it may do (granted and reported), pause or resume, a two-tap remove, and the steps waiting for a device. Adding a device shows the token once with a copy button and the setup commands (the token goes in a file, never inline). The laptop build offers only the "run the assistant" permission; phone is shown as coming soon. A step waiting for a device says "Waiting for your laptop to come online" instead of "Queued for a worker"; a step run by a device shows "Your device" rather than a raw id. When the server has hosts switched off the page says so instead of erroring. Typechecked, linted and unit-tested (logic only); **not yet viewed in a browser**, and a production build could not be run in the sandbox (no access to Google Fonts), so check the Vercel preview at 320-412 px.

- **Redeploy from the AWS console (ADR-078):** `infra/aws/redeploy.yaml` is a CloudFormation template for one CodeBuild project, `griffin-redeploy`. Starting it clones the public repo, **refuses any commit that is not on `master`**, applies database migrations, then runs `infra/ops/deploy-backend.sh` (same checksum, health-check and auto-restore as from a laptop). Dry run unless you set `MODE=apply`. No webhook: only you start it. Its role can update the API Lambda's code, start the runner, run the install script on that one instance, publish release files and read one secret; it cannot change the Lambda's configuration, IAM, or create anything. Tested locally: the master-only guard (tip, older master commit for rollback, unmerged branch, unknown commit, injection attempt), plan/apply/repeat of the migration step against a real database, bad inputs, missing secret; the template passes `cfn-lint`. **Not tested against AWS** (see 8.1d).
- **Sandbox and self-test (M3 foundation, ADR-075):** `internal/sandbox` runs a command under bubblewrap with only a scratch folder and an allow-list, network off, environment cleared (so the host token never reaches the agent). `SelfTest` plants a canary outside the allow-list and proves it is invisible from inside; a test shows a fake sandbox that isolates nothing is caught. `griffin-host --check` prints the result. **The bypass flag is still not used anywhere**; this is the gate it will need.

- **Permissions: policy engine, API, host sync, gateway, dashboard (M3, ADR-079):** `internal/policy` is the single decision point. Each rule narrows by device, capability, app, folder, contact and action class to off, ask or auto; the most specific rule wins and ties go to the stricter. Built-in floors the owner cannot lift: credential locations (`.ssh`, `.aws`, `.gnupg`, the host's own token folder, password stores, `/etc/shadow`) are always off, buying and calling never run automatically, and anything unrecognized is off. The **intent check**: an action outside what the plan step declared (another contact, app, class, capability, or a path outside its folder) asks even when the policy says auto, which is how "text from outside cannot trigger an action" (7.8) is enforced. Stored in `permission_policies` (migration 000006, RLS), edited at `/permissions` with a version so two devices cannot silently overwrite each other (409 on a stale save). Hosts can read it and never write it; the heartbeat carries the version so a host refetches only on change, ignores a policy that does not validate, and **denies everything outward until it has received one**. `internal/gateway` is the checkpoint a tool must call before acting: it stamps its own device on the action, fails closed, and writes an audit line (the shape of the action and the decision, never message bodies) to a 0600 file. The Permissions page offers per-class settings, app and contact rules, folders (read freely / never touch) and a time-boxed Full access (1-8 h, switches off by itself). `griffin-host --check` prints the policy version. Mutation-tested: removing the credential floor, the intent check, the buy/call cap, or the path-boundary check each makes tests fail.
  **Now called by the WhatsApp and web hands (ADR-080, ADR-081)**, so the policy is enforced for those; the browser, file and shell hands do not exist yet. Asking the owner works: a host can request an approval and is told on its next claim which actions were approved (`POST /api/v1/host/tasks/{id}/approval`). Full access is stored and time-boxed but does not yet change the sandbox. The network allow-list and uploading the audit log are not built.
- **WhatsApp hands (ADR-080):** read, send and list unread chats through WhatsApp Web in the laptop's own browser profile, falling back to the WhatsApp app on an Android phone attached with adb (or the other way round when asked: "from my phone"). Every send asks first by default and the approval covers that exact message to that person; contacts match by exact name or by phone number; nothing is retried after a send. Chat requests plan as two steps: an agent writes the action as strict JSON, then the device carries it out. Tested against a stand-in page in a real browser and a fake phone; **not yet run on the real account or phone**.
- **Web worker (ADR-081):** search and read public pages from a host, read-only, through the same permission gate; private and internal addresses are unreachable; fetched text is labeled untrusted; planned as search, read the top pages, answer from them with sources. Reliable search needs a Brave key or your own SearXNG; keyless DuckDuckGo is best effort.
- **Which device does what:** the capability a step requires decides it. Laptop: `agent`, `browser`, `files`, `shell`, `web`, `messaging` (WhatsApp Web). Phone: `phone_apps`, `sms`, `calls` and, when the laptop is off or asked to, `messaging` (WhatsApp app). Built today: `agent`, `messaging`, `web`. Not built: `browser`, `files`, `shell`, `phone_apps`, `sms`, `calls`.

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
| Laptop on: it is primary with full setup (`agy`, Claude Code, all tools) and a permissions dashboard. Laptop off: the phone is primary and reaches WhatsApp, Instagram and other apps within limits, asking approval for anything not permitted. Tasks can be followed up in chat. EC2 `agy` is only the brain in phone mode. | Designed (7.15 to 7.17, ADR-074 to 076); Android only; not built |
| Keep my resume context and role resume drafts in S3 (not repos) | Designed (7.15 Documents in S3); upload from Files works today |
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
- **P1 Parallel steps:** independent steps run together (agent concurrency now follows free memory and load up to a ceiling, ADR-073).
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
**Claude sub-agents and on-the-spot workers (2026-11-09): BUILT, off or untested against real runs as noted.** The owner chose: when Claude runs a job, Claude's own sub-agents do the splitting and Griffin's manager does not; when `agy` runs it, the manager writes a team of workers on the spot that imitates that. Built: `claudecli` lead mode (roster of three sub-agents on haiku, sonnet and opus with read-only tools, `--permission-mode dontAsk`, a per-step budget of 0.50 dollars that a step can lower but never raise past a 2.00 cap, effort and model from settings or the step, and the cost, models and sub-agents used recorded in the step's output); a `DynamicPlanner` that returns ONE lead step when `CLAUDE_LEAD` is on, otherwise asks the fast model for a team (2 to 6 workers with a role each, parallel where independent, a final join step), validates it strictly and falls back to the standard plan on any problem; the agy prompt tells each worker its role. Plans that need approval, a device, WhatsApp or web steps are never rewritten. A real run (with a Sonnet lead and a Haiku sub-agent) confirmed the flags and that both models ran; a trivial task cost 16 cents on Sonnet against under 1 cent on Haiku, which is why the budget cap exists. Settings: `CLAUDE_LEAD` (off), `CLAUDE_LEAD_MODEL` (sonnet), `CLAUDE_EFFORT` (medium), `CLAUDE_LEAD_BUDGET_USD`, `CLAUDE_BUDGET_CAP_USD`, `DYNAMIC_PLANNER` (on when a Gemini key is set). Not done: Claude runs only on the worker where `claude` is signed in (not yet on the laptop host); the quality of the fast model's teams has not been measured against the standard plans; per-step effort for agy is not passed.

**Original design notes (2026-11-09).** The `claude` CLI already supports `--agents` (a JSON roster of sub-agents, each with its own model and tools), `--model`, `--effort` (low to max), `--permission-mode`, `--allowedTools` and `--max-budget-usd`; Griffin's Claude provider passes only `--model` today. Two layers, so Claude decides what it is good at and Griffin keeps what crosses a device or permission boundary:
1. **Inside one step, on the laptop (Claude Code with the owner's login):** pass a small curated roster (a cheap fast agent for lookups and reading, a mid agent for analysis and writing, a strong agent for hard reasoning or review) and tell the lead to hand each piece to the cheapest agent that can do it and to run independent pieces in parallel. Guardrails Griffin sets and Claude cannot raise: tool allow-list by tier (the read tier gets read-only tools; acting needs the approval flow), never `bypassPermissions`, a per-step `--max-budget-usd` from a setting, a default `--effort`, and the existing time limit. Griffin sees one step; the roster and who did what are recorded in the attempt.
2. **Across steps (the orchestrator):** replace the keyword planner with an LLM planner (the fast model, which needs no cold start) that returns steps in a strict schema, each with the capability it needs and a model tier and effort suggestion. Griffin validates it (step count, dependencies, allowed tiers, owner caps) and falls back to the keyword planner on any failure. Tiers map to `--model` and `--effort` for Claude steps and to agy's flags for agy steps.
Cost and learning: each attempt records the model, effort, duration and the CLI's reported cost, shown in Attempts, with a daily cap the owner sets. Limits and unknowns: Claude sub-agents run only where Claude Code is signed in (the laptop), so with the laptop off a step falls back to `agy` without them; the quality of a planner's tier choices must be measured on real runs before it is trusted; Anthropic's terms for automated use of a consumer subscription must be checked before heavy use.
- **What exists:** a plan is already a set of steps with dependencies; independent steps can run on separate workers, as many as free memory and CPU allow, up to a ceiling of 4 by default (ADR-073; the old fixed cap was 2 on a ~3.8 GB instance).
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

### 7.15 Two modes: laptop-primary and phone-primary (brain vs hands)

The owner's ideal, in one line: **laptop on, the laptop does everything with full setup; laptop off, the phone is primary and reaches the apps; either way the owner controls the permissions and can keep talking to a task.**

The design handle that makes this work: separate the **brain** from the **hands**.
- **Brain:** where the agent session (`agy`, Claude Code) runs and reasons.
- **Hands:** any online device that can perform a capability: `browser`, `files`, `shell`, `phone_apps`, `sms`, `calls`, `messaging`.

| Laptop host | Phone host | Brain | Hands |
|---|---|---|---|
| online | any | **Laptop:** `agy` and Claude Code with the laptop's own logins, tools and files, inside the sandbox of 7.16 | Laptop (browser, files, shell, local apps); phone for `phone_apps`, `sms` and `calls` when it is online |
| offline | online | **EC2 `agy`, text-only:** plans, drafts, decides; never holds a tool or a login | **Phone host:** apps, SMS, calls, notifications, within the permissions of 7.16 |
| offline | offline | EC2 `agy`, text-only | None: text steps still run; steps that need hands show "Waiting for your laptop / phone" and resume by themselves |

- **Who is primary** is automatic: a host is *live* when its heartbeat is under 45 s old. While the laptop is live it gets all work and the EC2 is not woken (saves cost and cold starts). The owner can pin a mode in Settings ("always use phone", "never use laptop").
- **Capabilities, not machines:** each step lists what it *requires*; each live host advertises what it *can do*; a step is claimed by a live host that covers it. A phone-only step waits for the phone even while the laptop is primary.
- **Host API:** a small API (heartbeat, claim, events, complete) with a per-host key; hosts never get database credentials. Claim reuses `ClaimNextTask` with one added condition; presence and tokens live in a `hosts` table (migration 000005; an `agents` row has no place for them). The owner grants capabilities, the host reports what it can do, and only the intersection is ever used, so a host cannot escalate itself.

**The laptop host (Linux).** Runs `agy` and Claude Code as the owner's own installs. This is the one place the agent's own permission prompts may be skipped, and only inside a verified OS sandbox (7.16, ADR-075). Each task runs in its own scratch folder. The EC2 worker keeps refusing the bypass flag (ADR-063); that rule is unchanged.

**The phone host (Android only).** A small Android app that connects out to the Host API over HTTPS (no inbound port), with its own revocable token. It operates the apps the owner enabled through Android's accessibility service, takes a screenshot before and after every outward action, and sends the screen's text tree (not raw screenshots by default) to the brain.
- **iOS cannot be a host.** Apple does not let a third-party app operate other apps. On iPhone the options are Shortcuts into Griffin and nothing more; this mode needs an Android phone.
- **Instagram, WhatsApp and similar:** the official API or web version first (7.4). Driving the real app is last resort, and Instagram and WhatsApp both restrict or ban automated accounts. Griffin warns once per app, the owner accepts the risk per app, and sending starts as *ask*.
- **Speed warning.** In phone mode every UI step would round-trip to a cold `agy` on the EC2 (12-16 s per step, section 5), so a 20-tap flow would take minutes. Three mitigations are part of the requirement: (1) **skill replay** (7.12) runs a known flow with no model call; (2) the brain returns a **batch** of actions with checkpoints, not one tap at a time; (3) a **warm session** (plan 1.7). *Done when* the second "send a WhatsApp message" replays without a model call.

**Switching mid-task.**
- A task finishes where it started; new tasks route by the current mode.
- If the laptop drops while a **read or text** step runs, its lease expires and the step is requeued to the EC2 (or waits for the laptop if it needs hands).
- If a host drops during an **outward action** (send, submit, post, delete), Griffin does **not** retry it. The task goes to *Needs you* with the last screenshot ("laptop went offline during the send; check before retrying"). Double-sending is worse than waiting.

**Documents in S3 (not repos).** The laptop host one-way syncs `Documents/Griffin` (the career-context file `AJ_resume_context.md` and role resume drafts) to the owner's S3 area. Repos are never synced. The laptop is the single writer; versions are kept on the bucket. A phone-mode or EC2 run fetches the files it needs by presigned URL into its per-task scratch folder and deletes them when the task ends. An edit made in phone mode is saved as a new version that the laptop pulls (no silent overwrite). Encrypted at rest, per-user prefix, never written to logs, results or playbooks.

**Claude steps** run on a host with a Claude login (laptop, in laptop mode). With the laptop off they follow the owner's policy: wait, or fall back to text-only `agy` on the EC2 and say so. No API key, ever.

### 7.16 Permission dashboard and how it is enforced

**Where it lives:** Settings → Permissions in the web app. The cloud is the source of truth, so the owner can change it from the phone while the laptop is off. The laptop host caches the signed policy and enforces it locally. Hosts and agents cannot edit policy; only the owner's signed-in session can. A localhost-only dashboard is rejected: it fails when the laptop is off, and an agent running on that laptop could reach it.

**What the owner controls** (each is *ask*, *auto* or *off*; defaults are *ask* for anything outward or destructive):

| Dimension | Examples |
|---|---|
| Device | laptop host, phone host (each can be disabled or paused) |
| Capability | browser, shell, files, messaging, calls, phone apps |
| App | WhatsApp, Instagram, Gmail, Chrome |
| Folder | `Documents/Griffin` yes, `~/.ssh` never |
| Contact | auto for Mom, ask for everyone else |
| Action class | read, draft, send, submit, buy, delete |

Plus one switch on the laptop: **Sandbox** (default: scratch folder plus allow-listed folders), **Elevated** (one task, approved with the exact extra access shown) and **Full** (everything the owner's user can reach; time-boxed, default 2 h, expires by itself, shown as a banner). Anything not covered by a policy row asks. "Pause all automation" is one tap.

**Enforcement sits below the agent, not inside it.** `--dangerously-skip-permissions` removes the agent's own prompts, so the dashboard cannot depend on them. The host enforces with:
1. **An OS sandbox** (dedicated unprivileged user or `bubblewrap`) exposing only the scratch folder and allow-listed folders. Browser profiles, `~/.ssh`, cloud credentials and password stores are not mounted unless a policy row grants them.
2. **A sandbox self-test at startup:** the host plants a canary file outside the allow-list and refuses to start the bypass flag if the agent could read it.
3. **A tool gateway** for outward actions (message send, browser submit, phone tap on a send button, calls): each action is checked against the policy and against the **declared intent of its plan step**. A step "reply to X on WhatsApp" may send to X. An action that falls outside the declared intent asks, even where policy says *auto*. This is how 7.8's rule ("text read from outside can never trigger an action") is made mechanical rather than hoped for.
4. **A network egress allow-list** in the sandbox, with the same private-address refusals as 7.11.
5. **An audit log** with screenshots, readable in the app.

**Why not just trust the owner's "full access":** an agent that browses a page and holds an unsandboxed shell can be steered by that page. Full mode exists and the owner can turn it on; it is time-boxed and still passes the gateway's intent check.

### 7.17 Follow-ups: every task is a conversation
**Automatic continuation (2026-11-09, BUILT).** A message sent to `/api/v1/jj/messages` is judged: a chat conversation the page is already holding is one thread (unless that thread is still answering, then the message is a task of its own, as before); otherwise `DecideThread` scores finished threads from the last 24 hours on continuation words ("also", "now", "make it shorter"), editing words about things that exist ("the second paragraph"), pronouns with nothing new to point at, topic overlap with the earlier request and answer, and recency, minus fresh-request wording ("research", "what is"), and "new task" always wins. At 0.60 or more the message joins the thread and the response says why (`continued`); from 0.35 to 0.60 it becomes a new task and offers the earlier one (`suggestion`); below that it is simply new. `new_task` forces a fresh task and `continue_task_id` forces a choice. Not built: the chat page does not yet show the `continued` and `suggestion` notes or a button to override; the weights were tuned on made-up phrasings, not on real use.

**Status (2026-11-09): "Continue a finished task" BUILT; the rest PLANNED.** `POST /api/v1/tasks/{id}/follow-ups` adds a message to a finished task's thread (QUEUED or running tasks are refused with a 409). The thread root is recorded in two payload fields (`thread_id`, `follow_up_of`), so there is no migration. A plain question becomes a chat step and a request for work goes through the planner, both starting with the thread's transcript (the root's request and answer, then each earlier follow-up's; a long thread keeps its beginning and latest turns). Follow-ups are hidden from the main task list and shown under the task they continue, with a composer on the task page; a follow-up of a plan step continues the plan. Not built: questions a step can ask the owner ("Waiting on you"), grouping the chat page's separate "Chat:" tasks into one thread, push notifications.


Many tasks cannot finish in one shot. A task has a **thread**; the agent, the owner and approvals all speak in it.
- **P0 Questions, not just approvals:** a step can pause as *Waiting on you* with a question ("which contact? which resume?"). A reply in the thread resumes it.
- **P0 Continue a finished task:** "also add X", "make it shorter", "now send it" create a linked follow-up step that starts with the task's transcript and files.
- **P1 Session reuse:** the thread's context lives in the database (transcript and files), never only in a device's session. A warm `agy` or Claude session is an optimization: switching laptop to phone mode mid-thread loses nothing but speed.
- **P1 Notify:** a question or approval pushes a notification (ntfy) whose tap opens the thread.
- Approvals in chat (7.14) are the same mechanism.

---

### 7.18a Notifications and the spoken summary (2026-11-09, BUILT, not yet used on a phone)
When a top-level task finishes, fails for good, or an approval is needed, the owner's phone gets a push through **ntfy** (free; the phone runs the ntfy app and subscribes to a private topic; `NTFY_TOPIC`, optional `NTFY_SERVER` and `NTFY_TOKEN`, `APP_URL`). The body is a few seconds of speech about the task, worded by case: a short answer is read as it is; a long result is boiled down by the fast model (or by its first sentences without one); a failure says why and what to do; an approval names what is waiting; an empty result says there are files. Steps of a plan are not announced, and a task that will be retried has not finished. Tapping the notification (or its Listen button) opens `/listen/<task>`, which fetches `GET /api/v1/tasks/{id}/spoken-summary` and reads it aloud with the phone's own voice; browsers only let a page speak after a touch, so it tries once and otherwise shows one big Play button. Limits: ntfy.sh is a public service (the topic name is the secret; self-hosting or a token hardens it); a notification carries the summary text, which therefore passes through ntfy.

### 7.18 JJ as a voice assistant on the phone (the Siri job), and the Vault
**Status (2026-11-09): S1 BUILT, the rest PLANNED.** Built: the `manager_name` setting (`GET/PUT /api/v1/settings/manager-name`, Settings page, used by chat, titles, prompts and the web labels) and the fast voice route `POST /api/v1/voice/turn` (Gemini `gemini-flash-lite-latest`, about 1 s; falls back to the worker on any failure; work is handed off with the conversation so far). `HOSTS_ENABLED` and `GEMINI_API_KEY` are set on the production Lambda. Also built (laptop host, off unless `HOST_HANDOVER=1`): the **low-battery handover** (below). Also built: the **Vault** (below). Not built: the Android app, phone host, coding with no laptop, automatic recording of a miss when a task needs a file (the API and the apps exist; nothing calls it from a task yet). Not verified: the voice route has been exercised against a stand-in and the real Gemini API from a script, not from a phone.

**What a phone page cannot do.** The installed web app cannot listen in the background, cannot hear a name when the screen is off, cannot see incoming calls and cannot answer them. Android allows those only to a native app. So the Siri job needs a small **native Android app (Kotlin, installed from an APK, not from a store)**. The web app stays as it is for everything else.

**Not reverse engineering Siri.** Its internals are closed and not needed. The job is its features, each mapped to an Android mechanism:
| Siri feature | Android way | Needs an LLM |
|---|---|---|
| Wake by name, hands free | A foreground service listening on the device for the manager's name | No |
| Speak and be understood | Android speech recognition (on-device where the phone has it) | No |
| Call, text, alarms, timers, open an app, flashlight, volume, do-not-disturb, navigate | Standard intents and system services, run on the phone | No |
| Say who is calling | Phone-state listener plus a contacts lookup plus text to speech | No |
| Answer or reject a call by voice | The answer-calls permission (Android 9+) | No |
| Read missed calls and call history | Call-log permission (allowed for an installed APK, not for a store app) | No |
| Chat like a normal assistant ("JJ wassup") | A fast model reached through the API | Yes |
| Do real work (plans, research, coding, documents) | A task for the manager and the worker, the same as the chat today | Yes |

**Hard limits, stated plainly.**
- Android blocks other apps from recording or speaking into a normal call. Griffin can announce, answer, reject, place and log calls, but **cannot talk to the person on the line** over the SIM. A live AI conversation needs a telephony provider (7.14, M6).
- A short name such as "JJ" is hard for any listener: it fires on look-alike sounds. "Hey JJ" is safer, and a name of three or more syllables is best. Expect to tune it on the real phone.
- Listening all day costs battery. It runs as a visible notification and can be paused.

**Manager name.** A setting, `manager_name` (default "JJ"). It is the one place the name comes from: chat labels, prompts, task titles, spoken greetings and the wake name. Changing it changes all of them; the phone app rebuilds its listener from the new name, with no training step. Old tasks keep their stored text.

**Fast voice path (no cold start).** A voice turn goes to a new API route. A fast hosted model (Gemini, key kept on the server only) answers chat and small questions in about a second while the EC2 worker stays asleep. When the turn becomes real work ("find internships and draft applications"), the route creates the normal task and attaches the conversation so far (what was said and what was answered) so the worker continues from it and the owner does not repeat anything. Small phone actions (calls, timers, texts) never wait for any model: the app parses them itself and the permission engine (7.16) still gates every outward one. The spoken transcript goes to the model provider; the owner can switch the fast path off, and then chat waits for the worker as today.

**Phone as its own host.** The app speaks the host protocol (7.15): heartbeat, claim, complete, with capabilities such as `phone_apps`, `sms`, `calls`, `files`. It acts for the owner with no laptop at all.

**When the laptop is off.**
- Steps that need the laptop (files, browser, shell, coding) show "Waiting for your laptop" and resume on their own. This stays the default.
- Reading, reviewing and planning code can run on the worker from a GitHub clone with no credentials (read tier).
- Writing code with no laptop would need the worker to run with tools. That reverses "the EC2 stays text-only", so it is an **owner decision**: if allowed, it works only in a scratch clone, pushes a branch (never master), and the owner reviews it on the phone.

**Low-battery handover (built).** `griffin-host` watches the laptop battery (Linux `/sys/class/power_supply`) and escalates once per stage while discharging: at **15%** every open project (VS Code's open folders plus `HANDOVER_REPOS`) is saved to a new local branch `griffin/handover-<time>` with a `HANDOVER.md`; at **5%** running tasks are stopped and handed back to the cloud as retryable with the reason, and the branches are pushed; at **2%** one last save and push. Plugging in resumes work and the next episode starts a new branch. Safety: git plumbing with a temporary index, so the owner's branch, staging area and files are never touched; only a new branch is written and pushed, never forced; `.env`, keys, tokens, cloud credentials and files over 5 MB are left out (a modified secret-like file keeps its committed version); the note lists file names and never contents. `griffin-host handover-check` shows what would be protected; `handover-test` does a local-only save. The thresholds are 15/5/2 and not just 2 because a push needs time and a laptop at 2% can be gone in seconds. Limits: Linux only; it cannot save what is only in an unsaved editor buffer; a project with no remote stays on the laptop; the cloud agent that continues from the branch is not built (needs the owner's decision on coding with no laptop).

**The Vault (files you need on the phone) - BUILT.** `/vault` page, `griffin-host vault-scan` and `vault-sync`, API `GET/POST /api/v1/vault`, `/vault/misses` and the same under `/api/v1/host/vault` for a host with the `files` capability. Documents are artifacts with metadata kind `vault` (no migration); download and delete use the existing artifact endpoints. Encryption is AES-256-GCM with a PBKDF2-SHA256 key (600,000 iterations), the same bytes in Go and in the browser (a shared known-answer vector, also decrypted with an independent Python implementation). The server refuses an ID or finance document that does not arrive in that format. The browser fetches bytes through the API, so the bucket needs no browser access rules. Limits: documents up to 5 MB (the Lambda limit); the passphrase is held in memory only and cannot be recovered; the first protected document asks for the passphrase twice; the filename scan is by name only; **not yet exercised from a phone or against the live API**. An S3 area per user, `vault/`, with a manifest. The phone downloads a file by a short-lived link and opens it inside Griffin or saves it; a copy cached on the phone is removed after a few minutes.
- **Sensitive documents** (PAN, Aadhaar, passport, licence, bank papers) are encrypted on the device before upload with a key derived from a passphrase the owner holds. S3 holds ciphertext only. A lost passphrase means a lost file, by design.
- **How files get there without uploading by hand:** (1) the laptop host mirrors the folders the owner allows (`Documents/Griffin` with the career context, resume drafts); (2) a one-time **filename-only scan** of allowed folders lists likely documents (names like resume, cv, pan, aadhaar, passport, marksheet, certificate, offer, licence) and the owner ticks which to mirror; nothing is uploaded before that; (3) **learning from misses:** when a task or voice request needs a file that is not in the Vault, Griffin records it ("PAN card image"), and when the laptop is next on it finds the best match and asks "add this to the Vault?" with one tap; (4) the phone camera can scan a document straight into the Vault.
- Never uploaded without a tick: sensitive classes, anything outside allowed folders, repos. Contents never reach logs, results or playbooks.

**Order of work.** S1 (server, testable now): `manager_name` everywhere, the fast voice route with handoff. S2: the Vault. S3: Android app v1 (listener, speech, local actions, calls, texts). S4: phone as host. S5: coding with no laptop (needs the decision above). S6: calls through a telephony provider (paid, optional).

**Android app v0.1 (2026-11-09, BUILT and installed on the owner's OnePlus Nord CE4 Lite, Android 15; not yet exercised by voice).** `android/` (Kotlin, no Compose, minSdk 29): sign-in with the web app's own account (Supabase password grant, tokens encrypted with a hardware-backed keystore key); a foreground microphone service that listens with the phone's on-device recogniser and wakes on the manager's name (read from the `manager_name` setting; a sound-alike matcher that tolerates "J J", "jay jay" and one wrong letter in longer names, and only counts the name at the start of a sentence); local commands that need no model (call, text with a spoken confirmation, timers, alarms, flashlight, volume, open an app, answer, reject); conversation through `POST /api/v1/voice/turn` with the spoken reply and a hand-off of real work to `/api/v1/jj/messages` with the conversation; caller announcements by name through the call-screening role, with spoken "answer" and "reject". Verified: 16 JVM unit tests on the matcher and the command parser (one flaw found and fixed: "open source is great" opened an app); the app builds, installs and signs in against `cmd/griffin-devstub` (a local stand-in running the real voice handler, reached through `adb reverse`). NOT verified: anything that needs the owner's permission taps (microphone, contacts, calls, texts: OnePlus blocks granting them from adb), the wake word and speech recognition by voice, call announcements, and battery drain. Known limits: Android 15 will not let an app open another app from the background (opening apps may fail while the screen is off); timers and alarms are the app's own (not the Clock app's); the wake word is a limited-vocabulary match on the recogniser's text, so a short name can false-trigger; the speech recogniser stops while the assistant talks.

**Android toolchain (no Android Studio) - set up 2026-11-09.** JDK 17 (Temurin), the SDK command-line tools, platform-tools, Android 35 and build-tools 35.0.0, and Gradle 8.9 live in `~/Android` (about 1 GB; no sudo, no system changes; each download checked against its published checksum). `source scripts/android-env.sh` then `cd android && ./gradlew assembleDebug` builds the APK (about a minute); `adb install -r android/app/build/outputs/apk/debug/app-debug.apk` installs it on the phone over USB or wireless debugging. Logic that does not need a device is unit-tested on the JVM with `./gradlew test`. The system's JDK 26 is too new for Android's build tools, which is why JDK 17 is separate. An emulator is optional (KVM is available) and cannot do real calls or a real microphone, so the OnePlus is the test device.

**Needed from the owner:** a Gemini API key (kept in the Lambda's settings only); permission to install the Android SDK on the build machine before S3; the phone model and Android version; the S5 decision.

## 8. Operations

### 8.1 Ship
- `infra/ops/ship.sh` (dry run) then `infra/ops/ship.sh --apply`: restarts the idle worker, deploys the Lambda, pushes `master` (Vercel deploys the web app), then verifies each live.
- `infra/ops/deploy-backend.sh lambda|worker|all [--apply]`: dry run by default; the worker path checks the binary's sha256, keeps the previous copy and restores it if the health check fails.
- Neither script creates infrastructure, changes IAM or launches instances; neither may ever print configuration (use `--query` to name the fields any AWS call prints).

### 8.1b Deploying the host protocol (order matters)
1. Apply migrations 000005 and 000006 first (`griffin-migrate up`, or the redeploy build does it): the server refuses to start on a schema that is behind. Both only add tables, so the old binaries keep working until the new ones are deployed.
2. Deploy the API and then the worker as usual; host routes stay off until `HOSTS_ENABLED=true`. Until then nothing changes for the cloud runner, except that it no longer claims a step that declares `requires` (none exist yet).
3. Set `HOSTS_ENABLED=true` on the Lambda when ready.

### 8.1d Redeploy from the AWS console (no CLI, no laptop)
One-time setup (needs a person who may create IAM roles; this is the deliberate approval agents.md asks for):
1. Merge the pull request to `master` (Vercel then deploys the web app by itself).
2. Secrets Manager (region ap-south-1) > Store a new secret > Other type > plaintext `{"url":"postgres://..."}`: the **session pooler (port 5432) or direct** connection of a database user that may migrate. Not port 6543. Name it `griffin/migration-db-url`; copy its ARN.
3. CloudFormation > Create stack > Upload `infra/aws/redeploy.yaml` > parameter `DatabaseSecretArn` = that ARN (leave the others) > tick that it creates a named IAM role > Create.
Each deploy:
4. CodeBuild > Build projects > `griffin-redeploy` > **Start build** (this is a dry run: it prints the pending migration and everything it would do) and read the log.
5. Start build with overrides > Environment variable overrides > `MODE` = `apply` > Start. Other overrides: `TARGET` = `all` | `lambda` | `worker` | `none` (migrations only), `MIGRATE` = `no`, `COMMIT` = an older commit on master to roll back (the worker also keeps its own `.prev` copy and restores it if its health check fails).
6. Lambda console > `griffin-api` > Configuration > Environment variables: add `HOSTS_ENABLED` = `true` when you want devices switched on (the redeploy role deliberately cannot edit this).
Not verified against a real AWS account: stack creation, that the CodeBuild image has the tools the scripts need (`dnf`, `aws`, `python3`; `zip`, `unzip`, `file`, `git` are installed by the build), and the role's permissions being sufficient for every call in `deploy-backend.sh`. Run the dry run first; if a permission is missing the log names the call, and the fix is to add that one action to the role in `redeploy.yaml`.

### 8.1c Run the laptop host
1. In the app (or `POST /api/v1/hosts` with your JWT) register a host: kind `laptop`, capabilities `["agent"]`. The token is shown once; save it to a file with mode 0600.
2. On the laptop: `GRIFFIN_API_URL=https://... GRIFFIN_HOST_TOKEN_FILE=~/.griffin-host/token ./griffin-host --check` (prints what is granted and effective), then run it without `--check`. A sample user service is in `infra/host/griffin-host.service`.
3. This build backs `agent`, `messaging` (only when Chrome, Chromium or Brave is installed, or adb is installed) and `web`, and refuses other capabilities on purpose (a browser it cannot drive would receive steps it would answer as plain text). Grant `messaging` and `web` to the laptop in the app's Devices page.
4. WhatsApp, once: stop `griffin-host`, run `griffin-host whatsapp-login`, scan the code in the window (WhatsApp > Linked devices), then run `griffin-host whatsapp-check`; it reports whether you are signed in, how many chats it sees, and which page selectors stopped matching. For the phone fallback install `adb`, enable USB or wireless debugging, allow the computer, and keep the phone unlocked; `whatsapp-check` reports the phone too. Settings: `HOST_WHATSAPP` (`web,android` default, `android,web`, `web`, `android`, `off`), `WHATSAPP_BROWSER`, `WHATSAPP_PROFILE_DIR`, `WHATSAPP_HEADLESS=false` to watch it work, `ANDROID_SERIAL`, `WHATSAPP_PACKAGE` (`com.whatsapp.w4b` for Business).
5. Web search: set `BRAVE_SEARCH_API_KEY` (a free key works) or `SEARXNG_URL` for reliable search; `HOST_WEB=off` switches the web worker off. Outward actions are logged to `~/.griffin-host/audit.log` (mode 0600; the shape of each action and the decision, never message bodies).
6. First real use: ask for something harmless first ("check my unread whatsapp chats"), then one message to yourself ("send a whatsapp message to <your own number> saying test") and approve it in the app.

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
| **M1 Fast, listens, simpler** (2-3 d) | Quick wins, no new infrastructure | Worker `IDLE_SHUTDOWN_SECONDS=900`, `MAX_CONCURRENT_ANTIGRAVITY` is a ceiling (default 4) with adaptive admission; **done:** Listen + voice picker, tabs by kind, manager-purity test; fast path done; summarize-as-worker-step deferred (below) | Plain-ask p50 before/after; Playwright at 320-412 px |
| **M2 Host protocol + laptop host** (1-2 w) **BUILT: host API, claim by capability, presence and mode, wake suppression, `griffin-host` (text-only). Not built: live Terminal, "ran on" label in the app, Waiting-for-device UI, task threads (7.17)** | The hands exist | Host API, `cmd/griffin-host`, capability claim and routing (no EC2 wake while a device can work), waiting states, "ran on" label, live Terminal (after capturing one real `agy` stream), **primary-mode election from heartbeats (45 s), no-retry rule for outward actions on host loss, task threads with questions and follow-ups (7.17)** | Host runs a task; stop it and a text step finishes on the EC2 while a device step waits, then resumes. **Stop if** a host needs more than these endpoints |
| **M3 Laptop hands** (1-2 w) **BUILT: sandbox + canary self-test, policy engine with floors and intent check, policy storage/API/host sync, tool gateway with audit, Permissions page. **ALSO BUILT: approvals from a host, WhatsApp hands (Web, then the phone app over adb), the web search/read worker, chat requests planned into them.** NOT BUILT: hands for the browser, files and shell, network allow-list, audit upload, Full access changing the sandbox, bypass flag** | Browser, messaging, approvals, documents | **OS sandbox + startup canary self-test, tool gateway with intent check, Permissions dashboard in the web app (7.16), bypass flag allowed on the laptop host only after the self-test passes**; Playwright browser; trust settings (ask/auto per action and contact; outside text can never trigger an action); approve in chat; messaging: click-to-send first, WhatsApp Web second; Ollama drafting + refusal fallback; documents folder + sync; playbooks validated; click-to-call | E2E on a fake chat page; then a real run: read-only, then one approved message to your own number. **Stop sending** on any account warning |
| **M4 Internships** (3-5 d) | The headline journey | Search, shortlist into the tracker list, drafts from your documents, each submission approved in chat, screenshot + tracker row | Dry run on 3 postings, then one real approved application |
| **M5 The phone as a host** (2-3 w) | Phone apps and SMS, laptop on or off | **Android phone host app** (accessibility service, outbound-only Host API, per-app enablement, before/after screenshots) so phone mode works with the laptop off; while the laptop is on, Mobilerun/DroidRun over ADB and KDE Connect are the faster path (adopt, don't build); skill replay so common flows need no model call (confirm delivery by reading the sent state, not the exit code) | With your phone: list notifications, read a WhatsApp message, one approved SMS to your own number. **Stop if** apps block automation |
| **M6 Calls like companies do** (optional) | Calls without SIM cards | Telephony provider (rented number or SIP trunk) + audio stream + speech pipeline (LiveKit Agents or Pipecat): (1) speak-a-message calls, many recipients in parallel after you approve the list; (2) two-way voice agent; (3) caller ID: rented or your own verified number, never a number you do not own | Needs your approval of a paid provider; India rules (DLT, DND, consent, hours, AI disclosure) checked first. **Skip** if click-to-call is enough |

**Deferred decision (summaries):** the final answer is still stitched in code because every multi-step plan already ends in a worker-written synthesis step; a separate worker summarize step would add about 13 s for little gain. Revisit once live Terminals show whether it matters.

### Every rejected idea and what replaces it
| Rejected | Alternative | When |
|---|---|---|
| Tools, browser, logins or a vault on the EC2 | Devices that already have them; EC2 stays text-only | now |
| SIM calls, many at once; live AI calls | M6: telephony provider and a speech pipeline; concurrency is the provider's (calling ten people is ten API calls) | optional |
| WhatsApp automation risk | Click-to-send (prefilled `wa.me` link) first; WhatsApp Web second; the official Business Platform only for a business number (templates outside 24 h) | M3 |
| A cloud Android bridge (emulator the EC2 controls) | M5: an Android phone host that connects out; Mobilerun + ADB + KDE Connect while the laptop is on | M5 |
| Skip the agent's permissions on the laptop with no sandbox | Bypass flag only inside a verified OS sandbox; Full mode is time-boxed (7.16) | M3 |
| A permissions dashboard served from the laptop | Dashboard in the web app; the laptop caches signed policy (7.16) | M3 |
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
14. **Phone model and OS:** phone mode needs Android (7.15). Which phone, and is it the one you carry? On iPhone this mode is not possible.
15. Which apps first in phone mode, given WhatsApp and Instagram restrict automated accounts (suggested: WhatsApp via click-to-send, one read-only app, then decide on Instagram).
16. Full mode: default time-box (suggested 2 h) and whether it may ever be always-on.
17. Laptop sandbox: `bubblewrap` or a dedicated OS user (suggested: dedicated user, simpler to reason about on Linux).
18. When the laptop drops during a send: Griffin never auto-retries (7.15). Confirm.
9. Capacity for sub-agents: stay at 2 agents on the current instance, or approve a larger instance (a cost decision).
