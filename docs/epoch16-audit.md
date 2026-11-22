# Epoch 16 audit: internship discovery as it runs today (2026-11-22)

Evidence is from the code at `e6b219f` and read-only queries on the production database.

## 1. Discovery sources
| Source | Code | Runs in prod? |
|---|---|---|
| Internshala HTML (7 category pages) | `internal/sources/sites.go` `InternshalaPages`, `ParseInternshala` | Yes, via LISTINGS SYNC |
| Unstop public JSON (6 searches) | `sites.go` `UnstopSearches`, `ParseUnstop` | Yes, via LISTINGS SYNC |
| InternDock (company pages) | `internal/domain/interndock.go`, `sites.go` `Read` | Yes, via LISTINGS SYNC; also the older `internal/service/interndock_sync.go`, which appends to the text list (no routine queued) |
| LinkedIn feed scroll + Gemini extraction | `internal/hands/linkedin/scan.go`, `internal/hands/linkedinjob.go` | Routine died 2026-11-22 (lease reaped, no recurrence) |
| Model-written rows (agy) | `internal/domain/tracker_list.go` `TrackerPrompt`, `InternshipSources`; merged by `internal/service/tracker_lists.go` `Save` | Only if a tracker task is run by a model; none queued |
| ATS boards (Greenhouse, Lever, Ashby) | removed in `d57ec39` (owner's earlier instruction) | No |

## 2. Scheduled internship work
- Tasks with a `cadence` re-queue themselves on completion (`internal/service/task_engine.go` ~L271). Queued now: `LISTINGS SYNC` (daily 09:00 IST), `ALUMNI DM BATCH`, `ALUMNI OUTREACH TRACKER`, `COMMUNITY SCAN`.
- `schedules` table: `INTERNSHIP TRACKER (daily 08:00)`, `APPLICATION + RESUME TAILORING`, `STARTUP COLD EMAIL` are unpaused but `last_run_at` is NULL for all. **Nothing ticks this table** (the EventBridge tick is unverified; the cloud worker is asleep).
- Code-only routines run at claim time in `internal/service/claim_hooks.go` `RunInCode`, from the laptop host's claim (`internal/api/handlers/hosts.go`). **If the laptop is off, no routine runs.**

## 3. Filters, exclusions, thresholds
- `sources.Fits` (`sites.go:301`): title must match `IsInternship`, `domain.FitsOwner` must pass, pay ≥ `MinStipend = 10000` ₹/month (InternDock exempt). **This drops unpaid research roles and anything without a stated rupee stipend.**
- `domain.FitsOwner` (`interndock.go:74`): tech-title regex plus `notMyJob` (`interndock.go:69`), which **excludes front-end, UI, design, analyst, test and "content" titles outright**, and `InIndiaOrWorldwide` for location.
- `sources.Tier`: A means ≥ ₹40,000 (`domain.MinInternStipendINR`) with a floor ≥ ₹15k; B ≥ ₹15k; C the rest. **Ranking is stipend only.**
- Dedupe: `ON CONFLICT (user_id, company, role)` in `internal/repository/listing_repo.go`. No canonical URL and no employer identity, so the same role with a reworded title is a new row.

## 4. Paths that write to the canonical tracker
1. `ListingSync.Run` → `job_discoveries` (`listing_sync.go`), then `WriteListingsList` overwrites the text list.
2. `SyncInternDock` appends rows to the text list (`interndock_sync.go:88`).
3. `TrackerLists.Save` merges a model's `## Tracker` table into the list (`tracker_lists.go:152`).
4. The web Remove button edits the text list (`web/src/app/trackers/page.tsx`); for Internships it closes the listing.
5. `cmd/seed_tracker` seeds the list from local JSON.

## 5. Hardcoded or synthetic opportunities
- `cmd/register_application_approvals`, `cmd/register_referral_campaign`, `cmd/update_tier_a_pipeline`, `cmd/run_internship_pipeline`, `cmd/cleanup_duplicate_tasks` contain hardcoded companies (Postman, Modal Labs, HyperVerge, NexusFlow…). Two of them **`INSERT INTO` production `tasks`, `approvals`, `outreach_campaigns` and `outreach_targets` directly**.
- Production residue: 1 outreach campaign with 27 `drafted` targets; 9 approvals naming these companies (3 APPROVED, 6 REJECTED); 36 non-archived tasks (25 COMPLETED, 4 FAILED, 7 CANCELLED).
- `job_discoveries` is clean: 59 verified rows (46 Internshala, 13 Unstop) and 2 closed InternDock rows, all written by the sync.

## 6. ATS adapters
None. Greenhouse, Lever and Ashby were deleted; SmartRecruiters, Workday, Recruitee, Workable and career-page JSON-LD were never built. `internal/service/posting_check.go` `CheckPosting` is a generic GET that marks only 404/410 as missing.

## 7. Location and eligibility assumptions
- Location: Indian city names or "remote" with worldwide/APAC wording (`InIndiaOrWorldwide`). Internshala WFH maps to "Remote (India)".
- Graduation year, degree and work authorisation are **never read**. Eligibility is assumed.

## 8. Application preparation and approval
- `BatchApply` (`internal/service/batch_apply.go`) reads Tier rows from the **text list** and creates one `APPLY -` task per row via `domain.ApplyTaskFor` (`tracker_list.go:212`). A model then drafts it, the owner approves, and `FinishApprovedApplication` (`task_engine.go:639`) / `SUBMIT -` tasks finish it.
- `job_discoveries.stage` (verified → drafted → approved → submitting → confirmed) exists, but **only `closed` is ever set** (`listing_sync.go:113` is the only `Move` caller). The pipeline stages and the apply tasks are not linked.
- `domain.ResumeFiles` picks a resume by role focus. No grounded project selection, cover letter or missing-information list.

## What is broken, in one line each
1. Precision comes from regexes and a stipend floor, so good roles are dropped (front-end, research, undisclosed pay).
2. Recall is three aggregators only; no employer-first discovery and no ATS.
3. Ranking is stipend only.
4. No eligibility, evidence, snapshots or change history; failures are counted but not stored.
5. Routines depend on the laptop being on; the `schedules` table never fires.
6. Hardcoded fake opportunities were written to production by `cmd/` tools; residue remains.
