# Handoff: where Griffin stands (2026-11-13)

Read this first in a new chat. Everything below is committed on `master` (local; **nothing has been pushed**). `git log --oneline | head -25` shows the work from this session.

## What the owner wants
A fully working, public-ready personal AI workforce (JJ): uses the owner's own laptop and phone like a person (any app, no per-app APIs), runs outreach campaigns (cold mail, applications) in the background with rate limits and reply tracking, asks before anything is sent, and ships as a polished product. Owner rules (also in `~/.claude/CLAUDE.md`): no Claude co-author lines in commits, prefer existing libraries (shadcn etc.), surgical changes.

## Built and how well it is checked
| Area | Where | Checked with |
| :--- | :--- | :--- |
| Listener state machine; no volume changes; falls back to the standard speech engine when the on-device one lacks the language | `android/.../core/ListenerMachine.kt`, `Listener.kt` | 67 Android unit tests incl. fuzz; **confirmed on the owner's phone** (log shows "mic: listening") |
| Phone registers as a device + heartbeat | `ApiClient.phoneHeartbeat`, `AssistantService.deviceBeat` | Builds; shows in Devices not yet re-confirmed |
| Editable approvals + approval push | `PATCH /api/v1/approvals/{id}`, `domain.WithEditedText`, `web/src/app/approvals/page.tsx` | Unit tests; SQL run on a scratch Postgres |
| Discuss first, then "go ahead" | `internal/service/discuss.go`, `handlers/jj_chat.go` | Unit tests |
| Outreach engine (rows, limits, leased sends, replies, follow-ups, drafts, CSV) | `migrations/000007`, `domain/outreach.go`, `repository/outreach_repo.go`, `handlers/outreach.go`, `service/outreach_drafter.go`, `web/src/app/outreach` | Unit tests; lifecycle + 6-concurrent-senders test on real Postgres |
| Browser hand (stop before send, replay only the approved script) | `internal/hands/browser`, `hands/browserjob.go`, `cmd/griffin-host/browser.go` | Unit tests; **real headless Chromium against a local form**; NOT on Gmail/LinkedIn |
| Phone hand (accessibility service, server chooses steps) | `GriffinAccess.kt`, `PhoneRunner.kt`, `core/PhoneJob.kt`, `handlers/device.go` | Unit tests; digest matches Go byte for byte; **never run on a real app** |
| Reply watching on the phone (only awaited names) | `core/ReplyWatch.kt`, `NotificationReader.kt` | Unit tests |
| Launch polish | `LICENSE` (MIT), `README.md`, `compose.yaml`, `make setup`, CI for web+android, `web/.env.example`, error/loading/not-found pages | Web build passes |
| Docs | `decisions.md` ADR-112..117, `evolution.md` Epoch 12, `docs/PRD.md` 7.22-7.23, `agents.md`, `docs/ROADMAP.md` | n/a |

Test status at last run: Go `./internal/...` all pass; web 182 pass; Android all pass; `tests/` outreach + migrations pass on a scratch DB.

## Immediate next step (blocked on the owner)
Phone control is not on. Android blocks accessibility for sideloaded apps. The owner must: Settings > Apps > Griffin > three dots > **Allow restricted settings** (Android treats this as the user's own decision; an attempt to set it over adb was refused by the permission system and must not be worked around), then Settings > Accessibility > **Griffin phone control** > on, then on the web Devices page grant the phone "Use any app on the phone". After that, give it one harmless job ("on my phone, open Notes") and watch `adb logcat` / screenshots. The phone is attached over USB debugging (adb at `~/Android/Sdk/platform-tools/adb`, device `e47a0795`, the installed app is a debug build so `adb install -r` keeps sign-in).

## Not built yet
- Onboarding wizard in the web app (sign in, add devices, grant permissions, connect the browser profile).
- Dockerfiles for the server and web app (only the database is in `compose.yaml`).
- Laptop-side reply polling (a browser job that checks Gmail/LinkedIn inboxes and posts to `/outreach/replies`). The endpoint and the manual "Replied" button exist.
- Splitting one chat request ("cold mail 30 founders") into a researcher and a sender over a campaign. Today it becomes one browser job.
- Verification on real Gmail/LinkedIn (needs `griffin-host browser-login`, `HOST_BROWSER=on`, `GEMINI_API_KEY`).

## Before publishing
- AWS account ids were scrubbed from docs only; they remain in `infra/*.sh`, `infra/aws/redeploy.yaml`, `internal/config/config_test.go`, `tests/s3_live_test.go` (deploy config; parameterise carefully).
- Scan full git history for secrets (gitleaks/trufflehog); consider a fresh-history public repo.
- **Rotate** the Supabase database password that was pasted into an earlier chat, and the Vercel OIDC token in `web/.env.local`. `.env copy` (untracked) may hold secrets: never commit it.
- Audit dependency licences (GSAP). `scripts/generate_matrix.py` and the two `.xlsx` matrices contain personal paths/data.
- LinkedIn and similar sites restrict automated account use: limits are conservative and approval is mandatory; keep it that way.

## How to build and test (gotchas)
- **Android needs JDK 17**: `JAVA_HOME=/home/adhyan/Android/jdk-17 PATH=$JAVA_HOME/bin:$PATH ./gradlew --offline -q testDebugUnitTest assembleDebug` from `android/` (system Java 26 fails with a bare `26.0.2.1`).
- Go: `go test ./internal/... ./cmd/...`. Integration tests in `tests/` need `GRIFFIN_TEST_DB_URL` pointing at a database where roles can be created; the local Supabase `postgres` user can't in the main DB, but a **fresh scratch database** on the same server works for non-role tests (create, run, `DROP DATABASE` after). The role-based RLS tests (e.g. `TestEditPendingApprovalRebindsTheDigest`) still need a superuser URL and were never run.
- Web: `cd web && npx tsc --noEmit && npx vitest run && npx next build`.
- Local run: `make setup` then `make run` and `cd web && npm run dev`.

## Repo state
- Default branch `master`; all other local branches are already merged into it (`git branch --no-merged master` is empty). Many old branches and `.claude/worktrees/*` remain; deleting them is the owner's call.
- Commits must show only the owner as author (no Co-Authored-By trailers).
