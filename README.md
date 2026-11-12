# Griffin

**A personal AI workforce that uses your own laptop and phone.** You talk to a manager (named JJ by default). It plans the work, hands it to specialists, and asks you before anything leaves your hands: a message, an application, an email.

Griffin is not a chatbot with plugins. It acts on *your* devices, in *your* signed-in apps, in the background, and every outward action waits for your yes.

## What it does

- **Outreach campaigns.** "Find 30 founders and cold-mail them." Griffin builds a list of people, drafts each message, and waits. You approve or edit each draft; it sends within daily limits, tracks who replied (a separate list), and drafts follow-ups for the ones who didn't. Export to CSV any time.
- **Any website, through your browser.** The laptop agent works your signed-in Chrome (Gmail, LinkedIn, an application form) step by step. It **stops before sending or submitting**, shows you exactly what will be typed and which button ends it, and replays only the script you approved.
- **Chat apps by voice.** Say "hey JJ, reply to Sam saying I'm late" on the Android app. Replies go through the chat app's own notification, so nothing opens on screen.
- **Talk a job through first.** "Let's discuss the internship filter first." Refine it, then say "go ahead".
- **You stay in control.** Approvals are bound to the exact action (a digest of recipient and text). Editing a draft re-binds it. A permission dashboard sets what each app may do. The agent's own device can never approve its own message.

## Honest status

Built and tested: the task engine, approvals (with editing), the outreach engine, the browser hand (against a real headless Chromium and a local page), the web app and the Android assistant. **Not yet tried against real Gmail or LinkedIn, and not yet confirmed on a physical phone.** Not built: the phone-side device-use hand and inbox reply watching. See [`docs/ROADMAP.md`](./docs/ROADMAP.md) for the full table.

**Be careful with automation of accounts.** LinkedIn and some other sites restrict automated use. Griffin's limits are conservative by default and approval is mandatory, but you are responsible for your accounts.

## Architecture

```
 Web app (Next.js)  ─┐                      ┌─ Laptop host (griffin-host): browser, WhatsApp Web, desktop, web search, agent
 Android assistant  ─┼─►  Griffin API (Go) ─┤
 (voice, notifications)   │  task engine (DAG), approvals, outreach, policy
                          │  PostgreSQL + Row Level Security
                          └─ Cloud worker (on demand) for text-only work
```

- **API / task engine** (`cmd/griffin-server`, `internal/`): a Go monolith. Tasks form a dependency graph; workers lease them with `SELECT ... FOR UPDATE SKIP LOCKED`. Every table is protected by Row Level Security.
- **Hosts** (`cmd/griffin-host`): your laptop (and the phone) register as devices, send heartbeats, claim the steps they can do, and enforce your permission policy locally.
- **Hands** (`internal/hands`): WhatsApp (Web and adb), Instagram (adb), web search, desktop control and the browser hand.
- **Web** (`web/`): Next.js 16, React 19, Tailwind 4, shadcn/ui.
- **Android** (`android/`): Kotlin voice assistant with an on-device wake word, built with plain Gradle.

## Run it locally

You need Go, Node 22+, and Docker (for the local database).

```bash
make setup        # creates .env and web/.env.local, starts Postgres, applies migrations, installs web deps
# edit .env: add SUPABASE_JWT_SECRET (or JWKS URL) and ALLOWED_USER_EMAILS
make run          # API on http://localhost:8080
cd web && npm run dev   # web app on http://localhost:3000
```

Sign-in uses Supabase auth; see `.env.example` for each variable. To use your own devices:

```bash
make run-host                      # the laptop host (see Makefile for HOST_* options)
griffin-host browser-login         # one-time: sign in to Gmail, LinkedIn... in Griffin's own browser profile
# then set HOST_BROWSER=on and GEMINI_API_KEY and restart the host
```

Android: build with JDK 17 (`JAVA_HOME=/path/to/jdk-17 ./gradlew assembleDebug` in `android/`). The app registers itself under Devices after you sign in.

## Tests

```bash
make test-v                                   # Go unit tests
GRIFFIN_TEST_DB_URL=postgres://... go test ./tests   # integration tests; needs a superuser database URL
cd web && npx vitest run && npx tsc --noEmit  # web
cd android && ./gradlew testDebugUnitTest     # Android (JDK 17)
```

## Migrations

| Migration | Adds |
| :--- | :--- |
| `000001` – `000002` | Core schema (profiles, agents, tasks, dependencies, approvals, artifacts) and Row Level Security |
| `000003` | Orchestration, plans, delegation and the agent factory |
| `000004` | Object storage metadata for artifacts |
| `000005` | Hosts (laptop and phone devices) |
| `000006` | The owner's permission policy |
| `000007` | Outreach campaigns, people and their audit trail |

Migrations apply on server start and in the test suite; `make migrate` applies them to a local database by hand.

## Documentation

- [`decisions.md`](./decisions.md): every architecture decision (ADR-112 to 117 cover this release).
- [`docs/PRD.md`](./docs/PRD.md): what the product is and why; sections 7.22 and 7.23 cover outreach and discussion.
- [`evolution.md`](./evolution.md): how the design changed over time.
- [`agents.md`](./agents.md): the operating manual for AI agents working on this repo.
- [`docs/ROADMAP.md`](./docs/ROADMAP.md): what is verified, what is not, and what is next.

## License

No license has been chosen yet; until one is added, all rights are reserved.
