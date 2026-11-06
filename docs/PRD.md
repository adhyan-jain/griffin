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
- Frontend: product grade, phone first; use the design skills and existing libraries (shadcn/ui, Radix, Tailwind).

---

## 3. Architecture (as built)

| Part | Technology | Where |
|---|---|---|
| Web app (PWA) | Next.js 16, React 19, Tailwind v4, shadcn/ui, React Three Fiber, GSAP, Lenis | Vercel, `https://griffin-pwa.vercel.app`, auto-deploys from `master` |
| API | Go, Lambda (arm64) behind API Gateway HTTP API | `griffin-api`, ap-south-1 |
| Database and auth | Supabase Postgres with row-level security; Supabase Auth (JWKS) | pooler connections; `griffin_app` (runtime), `griffin_system` (system) roles |
| Files | S3 bucket, per-user key prefix | `griffin-artifacts-220115213279-ap-south-1` |
| Worker ("runner") | Same Go binary in worker mode, systemd service, runs Google Antigravity CLI `agy` 1.5.2 | One EC2 instance `i-070222a98aa0cadce`, scale-to-zero (wakes on demand, stops after 5 idle min) |

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
- **P0** Risk level on every action; high and critical always need approval; the approval shows exactly what will happen.
- **P0** Rotate the credentials exposed on 2026-11-06 (section 8.3).
- **P1** Allow-lists, spending caps, quiet hours, kill switch, audit log.

### 7.6 Reliability and operations
- **P0** Replace the worker; add the worker step to `ship.sh`.
- **P0** Version check: the worker reports its build; the app warns when it is older than the API.
- **P0** Readable API logs, to diagnose wake failures.
- **P1** Alerts: plan stuck, runner awake but idle, wake failed, agent sign-in expired.

### 7.7 UI/UX
- **P0** Live progress on running tasks (the live per-task view).
- **P1** Push notifications when something needs you or finishes.
- **P1** Confirm smoothness on a real mid-range phone (60 fps Full, 30 fps Lite).
- **P2** One home view: what is running, what needs you, what finished.

### 7.8 Models, guardrails and "all access"
"Guardrails" means three different things, and they should not be treated the same:

| Kind | Who sets it | Can we remove it? | Should we? |
|---|---|---|---|
| **Model refusals** (a hosted model declines some wording or topics) | The model vendor | Only by using a model that does not refuse (self-hosted open weights) | Yes for *drafting your own messages*, in your own voice |
| **Approval gates, allow-lists, spending caps** | Griffin (ours) | Yes, they are our code | **No.** They are not about what the model *says*, they are about what it can *do*. An agent with full access and no confirmation can be steered by anything it reads (a web page, an email, a chat message: "prompt injection") into sending messages, deleting files or paying, and it makes mistakes at machine speed |
| **Platform rules** (WhatsApp, LinkedIn terms) | The platforms | No | Respect them; account bans are the cost |

**Recommended design: separate the voice from the authority.**
- **Voice (writes text):** a self-hosted open-weights model (Ollama) with no refusals, used only to *draft* text in your tone: replies, messages to friends, any wording you want. Your words are your call. Output is a draft; Griffin never needs the model to be "safe" for this job.
- **Authority (does things):** a strong hosted agent (agy now, Claude when enabled) does planning and tool use. Its permissions are ours: allow-lists, per-action approvals and a kill switch, tuned by **trust levels**, not by removing them. Example: auto-send replies to contacts on your allow-list; always ask before money, deletes, new contacts or anything irreversible.
- **"Give it all access"** is therefore delivered as *more trust over time* (allow-lists you grow, an auto mode for a class of action after you have watched it work), never as "no checks".

**Self-hosted model options (decision needed):**
| Option | Pros | Cons |
|---|---|---|
| Ollama on **your own computer** (reached from the worker over a private tunnel such as Tailscale) | No new cloud cost, no GPU bill, your data stays home | Your machine must be on; quality limited by your hardware |
| Ollama on a **cloud GPU instance** | Always available | Costs far more than the current setup, which breaks "scale to zero, no unnecessary infrastructure"; needs a new instance (needs your explicit approval) |
| Hosted uncensored model API | Easy | A new paid dependency; conflicts with "no paid API fallback" unless you approve it |

**Honest limits:** open-weights models are weaker than the hosted agent at following long tool instructions, so they are a poor choice for *driving* complex tasks and a good choice for *writing*. The routing built in ADR-059 already separates steps by what they need; the voice model would be a third engine for "draft" steps. Not building anything aimed at harassing or abusing people at scale; your own messages to your own contacts are yours.

### 7.9 Using it from your phone
- **Front end:** `https://griffin-pwa.vercel.app`. It is an installable PWA: open it in Chrome (Android) or Safari (iPhone) and choose **Add to Home Screen**; it then opens full screen like an app. Sign in with your Supabase account.
- **"Use my Claude from my phone":** the Claude app on your phone cannot be driven by Griffin, and a phone cannot lend its logged-in session to a server. What *is* possible: sign the Claude CLI in **once on the worker** (device login with your own account, no API key), after which Griffin uses that login for language steps. That is a credential living on the worker (as sensitive as a password; encrypted disk, no remote access except the platform's session manager). Check the Claude terms for automated use of a consumer subscription before relying on it.
- **Your apps from the phone:** see section 7.4: Layer 2 (web versions) and Layer 3 (an Android bridge) are how Griffin operates apps; iOS only supports Shortcuts into Griffin.
- **Credentials on the worker are unavoidable** for anything that logs in for you; the design choice is to store them in the cloud parameter store (encrypted) or as saved browser sessions, never in prompts, files or logs.

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

## 9. Plan to finish

**Order of work, what each step needs, and how we know it is done.** Effort: S under a day, M a few days, L about a week or more. "You" marks something only the owner can do or decide.

### Phase 0: Stabilize (do first; unblocks everything)
| # | Work | Effort | Needs | Done when |
|---|---|---|---|---|
| 0.1 | Ship the waiting commits (`ship.sh --apply`) | S | You run it | Live checks pass: new routes 401, web has the file viewer and Save to list |
| 0.2 | Replace the worker binary (`deploy-backend.sh worker`, dry run first) | S | You approve and run it | Worker reports the new build; plans finish the instant their last step ends; the runner reaches idle shutdown; saved files and compaction log lines appear |
| 0.3 | Add the worker step to `ship.sh` and a build-version check (worker reports its commit; the app warns when it is older than the API) | M | none | A stale worker is visible in Settings |
| 0.4 | Rotate the exposed credentials (PRD 8.3) | S | You | Sign-in works, one task completes, old values retired |
| 0.5 | Make API logs readable: grant read on `/aws/lambda/griffin-api` only (a narrow policy), or ship logs to a place already readable | S | You decide on a narrow log-read policy | We can see why a wake failed |
| 0.6 | Diagnose why the runner sometimes does not wake (likely the Lambda role's EC2 permission or a cooldown) using 0.5 | S | 0.5 | A submit on a stopped runner starts it, proven twice |
| 0.7 | Delete your old tasks from the app (Tasks, Select all, Delete); check on your phone | S | You | Clean list; you report what feels off |

### Phase 1: Fast
| # | Work | Effort | Needs | Done when |
|---|---|---|---|---|
| 1.1 | One-step fast path | done | ship | Plain ask returns in one agent run |
| 1.2 | Warm window: `IDLE_SHUTDOWN_SECONDS=900` | S | worker env change | A second task within 15 min starts without a boot |
| 1.3 | Two agents at once: `MAX_CONCURRENT_ANTIGRAVITY=2` (hard cap 2; the instance has ~3.8 GB RAM and the worker uses well under 1 GB) | S | worker env change | Two independent steps overlap in the log |
| 1.4 | Capture one real streaming run (`agy --output-format stream-json`) on the worker to learn the event format | S | You allow one test prompt | A saved sample of real events |
| 1.5 | Streaming to the app: events become task activity as they happen; the app shows them live (server-sent events, falling back to the existing adaptive polling) | L | 1.4 | First words visible within 3 s of the agent starting |
| 1.6 | Effort tiers: low reasoning effort for simple steps, default for hard ones (`AGY_EFFORT`, per-step) | S | 1.4 to judge quality | Simple answers are measurably faster with no quality loss on a sample of 10 |
| 1.7 | Reuse a warm conversation for follow-ups (`--conversation`) | M | 1.4 | A follow-up skips the cold start |
| 1.8 | Timing dashboard: p50 and p95 per step type from the attempts table, in Settings | M | none | Numbers on screen match section 10 targets |

### Phase 2: Real work (research with tools)
| # | Work | Effort | Needs | Done when |
|---|---|---|---|---|
| 2.1 | Turn on `AGY_TOOLS_ENABLED=true` on the worker; run three read-only research tasks while we watch the logs | S | You | Cited sources; nothing changed; flags confirmed under `--sandbox` |
| 2.2 | Live per-task view (built on 1.5): steps, pages visited, commands | M | 1.5 | You can watch a running task from your phone |
| 2.3 | Playbooks observed: run the same kind of task twice | S | 2.1 | Second run is faster and shows the notes |
| 2.4 | Browser automation: headless Chromium (Playwright) with a per-task profile on the worker; screenshots attached to the task | L | 2.1 | A task opens a site, reads a page and attaches a screenshot |
| 2.5 | Credentials vault: SecureString parameters, worker fetches at use time, never in prompts or logs | M | 2.4 | A login works with the secret absent from every log |
| 2.6 | Code sandbox for data tasks | M | 2.1 | A CSV task runs isolated and returns a file |

### Phase 3: Acts for you
| # | Work | Effort | Needs | Done when |
|---|---|---|---|---|
| 3.1 | Approval UX: show the exact action, the page and a screenshot before and after | M | 2.4 | You can approve from the phone with full context |
| 3.2 | Profile (resume, links, standard answers): a table and a Settings screen | M | a migration (approval) | Tasks can read it; nothing else can |
| 3.3 | Tasks append to lists themselves: a `list` field on a step; on success a row is added | M | none | The internship tracker grows without you |
| 3.4 | Web Push for "needs approval" and "finished" | M | VAPID keys and a subscription table (migration) | Phone notification arrives |
| 3.5 | Gmail and Calendar connectors (OAuth): read and draft; send with approval | L | You create a Google Cloud OAuth client | Summary of today's mail; a drafted reply waiting for approval |
| 3.6 | The internship journey end to end (acceptance journey 1) | M | 2.4, 3.1 to 3.3 | Shortlist, drafts, approved submissions, screenshots, tracker rows |
| 3.7 | Trust levels: allow-lists and an auto mode per action class; kill switch; audit log | M | none | You can pause everything in one tap; every action is logged |

### Phase 4: Your apps
| # | Work | Effort | Needs | Done when |
|---|---|---|---|---|
| 4.1 | WhatsApp Web: pair once (QR), keep the session on the worker; read, summarise, draft; each send approved | L | You scan the QR; accept the terms risk | Acceptance journey 2 passes |
| 4.2 | Android bridge for one chosen app (ADB over a secure tunnel, UI automation, screenshots) | L | You choose the device (spare phone, your phone, or an emulator) | One real action in one app, with before and after screenshots |
| 4.3 | iOS Shortcut "Send to JJ" and a share-sheet action | S | none | Anything shared from an iPhone arrives as a task |

### Phase 5: Models and memory
| # | Work | Effort | Needs | Done when |
|---|---|---|---|---|
| 5.1 | Claude on the worker for language steps (one-time CLI sign-in; `CLAUDE_ENABLED=true`) | S | You sign in | A drafting step runs on Claude; falls back to agy if signed out |
| 5.2 | Voice model: Ollama on your own machine over a private tunnel, used only for drafting text in your tone | M | You choose where it runs (section 7.8) | A "draft my reply" step uses it and returns a draft |
| 5.3 | Engine policy per step: tool steps to agy, hard writing to Claude, tone drafts to the voice model | M | 5.1, 5.2 | The router records which engine ran each step |
| 5.4 | Search across results, files, lists, playbooks | M | none | One search box finds anything |

### Cross-cutting (every phase)
- **Tests:** each feature ships with Go tests (real Postgres) and a check added to `e2e-real.mjs`; CI (`.github/workflows/ci.yml`) must stay green.
- **Docs:** update this PRD (section 4) and `decisions.md` with every change; `evolution.md` at phase ends.
- **Security:** no secret in any log, prompt, result or chat; deploy tooling never prints configuration; secrets only in the parameter store.
- **Verification:** nothing is called done until it is checked live (API route, bundle, worker log), and UI changes are checked at phone and desktop size; the owner confirms feel on a real phone.

**Acceptance journeys**
1. *"Apply to every internship I'm eligible for this week."* Search job boards (read); shortlist into the Internship tracker with links and deadlines; draft each application from your profile; you approve each submission (act); each submitted one gets a screenshot and a tracker row. Next week it reuses the playbook and is faster.
2. *"What did my family group say on WhatsApp today? Draft replies."* Summary and drafts; nothing sent without per-message approval (or auto-send for allow-listed contacts once you enable it).
3. *"Plan a weekly status report."* One step, streamed, under 15 s.
4. *"Compare these three laptops under ₹80k with sources."* Cited table, saved as a file.

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
6. Where a self-hosted voice model would run (your own computer over a tunnel, a cloud GPU instance, or not at all) and whether the trust-level design in 7.8 is what you want in place of "no guardrails".
7. A narrow read-only grant for the API's logs (0.5), so failures can be diagnosed.
