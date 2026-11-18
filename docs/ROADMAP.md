# Roadmap and honest status

## Built in Epoch 12 & 13, and how well it is checked
| Piece | Checked with | Not yet checked |
| :--- | :--- | :--- |
| Listener state machine, no volume changes | 66 Android unit tests incl. a 3,000-sequence fuzz test | On the phone: that the ringer no longer flips |
| Phone registers as a device | Builds; server API already tested | On the phone: that it shows live in Devices |
| Editable approvals + approval push | Unit tests, RLS suite (`TestEditPendingApprovalRebindsTheDigest` passing) | A real device APNs/FCM push |
| Discuss first, then go ahead | Unit tests | A real conversation |
| Outreach engine | Unit tests; lifecycle and a concurrent-senders test on a real Postgres | Under RLS in production; with a real sender |
| Browser hand | Unit tests; a real headless Chromium against a local form (stage, replay, mismatch refusal) | Gmail, LinkedIn or any real signed-in site |
| Strict Android contact disambiguation | Android unit tests with Gradle 8.9 / JDK 17 (`Contacts.resolve`) | Live phone SIM dialer execution |
| Dual execution routing (Claude CLI + Antigravity) | Go unit tests across router, domain, and worker packages | Production AWS EC2 scale-to-zero runner invocation |
| Dynamic planner observability & degraded indicators | DynamicPlanner Go test suite with brain fallback assertions | End-to-end multi-turn web chat orchestration session |
| In-flight task steering prompt injection | Antigravity and Claude prompt builder unit tests | Live streaming runner task steering |

## Not built yet
- **Phone-side device use** (Android accessibility service executing UI steps, claiming `phone_ui` tasks). Until then the phone does WhatsApp/Instagram replies through notifications and the laptop does the rest.
- **Reply watching**: nothing yet polls an inbox page or turns a Gmail/LinkedIn notification into a `/outreach/replies` call. The endpoint and the Replied list exist.
- **Automatic campaigns from chat**: the planner routes "cold mail ..." / "on LinkedIn ..." to one browser job. Splitting a request into a researcher and a sender over a campaign is the next step.
- **Onboarding wizard** in the web app, a Dockerfile for the server and web app (only the database is in `compose.yaml`).

## Before publishing
- (Done) MIT LICENSE added.
- (Done) AWS account ids, the instance id and the bucket name were removed from files and from git history; the infra scripts now take them from the environment.
- Scan the full git history for secrets (gitleaks or trufflehog) and decide whether to publish a fresh-history repo. Rotate the Supabase database password that was pasted into a chat and the Vercel token in `web/.env.local`.
- Audit dependency licences (notably GSAP).
- `scripts/generate_matrix.py` and the two copies of the testing matrix `.xlsx` still hold personal paths or data.

## Ideas
Per-campaign analytics (reply rate, best send times); A/B testing of message openers; one-tap resume tailoring per person; a spoken daily briefing; QR-code pairing for the phone; ready-made campaign templates; a demo mode with fake data; an MCP server exposing the laptop and phone hands to other agents; a browser extension that sends a lead from any page.
