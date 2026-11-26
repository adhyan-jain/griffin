# Independent audit of the VIT Alumni Prospecting campaign — 2026-11-26

Scope: Antigravity's uncommitted "VIT Alumni Prospecting & Startup Network" work on `master`. That covers migration 000026, `internal/sources/{alumni,companies}`, `service/vit_campaign.go`, `service/outreach_drafter_vit.go`, `repository/campaign_repo.go`, `handlers/campaign.go`, and the VIT section of `web/src/app/outreach/page.tsx`. Its implementation report was not trusted; every claim below was checked against the code, the database or a live source.

## 1. Verdict

**This was not a prospecting system. It was a seeded dashboard of fabricated people, and it could hand LinkedIn messages to automation.** Nothing in it came from research. All of it is now removed or repaired. What remains is an honest, empty pipeline that only accepts evidence-backed records.

**Not ready for a larger outreach campaign.** Zero real, verified alumni exist in any database I could inspect. The pipeline is now safe to fill, but it hasn't been filled.

## 2. The "first research run" dataset

| Question | Answer |
|---|---|
| Where did the records come from? | Go literals in `sources/alumni/acmvit.go` (12 people) and `vit_vellore.go` (12 people), written by `POST /api/v1/campaigns/vit-alumni/seed`. No research run, search or fetch produced them. |
| Unique real people discovered | **0** from any research. The 23 "alumni" plus 1 "current student" were hand-written. |
| Verified VIT Vellore / eligible / ACM / tech / ops / startup / pay evidence / contact route | **0 / 0 / 0 / 0 / 0 / 0 / 0 / 0** with real evidence. The literals *claimed* all 24 were `confirmed_vellore`, and 12 of them ACM `confirmed`. |
| Synthetic records presented as production | All 24. Two were labelled in comments as test cases ("Negative cutoff test case", "Chennai campus -> negative campus test case") but shipped in the production list. |
| Local databases | Every local DB is at migration 000021 or has no person rows. No campaign data exists locally. |
| Production database | **Not inspected.** The read-only check was blocked by the session's permission policy. See §11. |

**Source tracing (live, 2026-11-26)**
- `https://acmvit.in/team` → **404**. `https://acmvit.in/about` → **404**. These are the "sources" cited for 4 people. The home page loads but names none of them.
- On GitHub (`gh api`), `ACM-VIT/acm-vit.github.io` has contributors `saurav-bhagat`, `rishiraj824`, `ashwini0529` and `awasthishubh`, and `reverse_coding` has only `Sourish1997`. So four names likely belong to **real people**. Their invented employers, titles, roles ("Technical Director", "Chapter President"), tenures, graduation dates and LinkedIn URLs, however, have no source.
- Several cited repos don't contain the person at all. `time_table_analyser` (cited for "Yash Kumar Verma") has one contributor, `awasthishubh`. `reverse_coding` (cited for "Rishi Raj" as "author and lead maintainer", and for "Shivam Verma") has one contributor, `Sourish1997`.
- LinkedIn slugs such as `riya-sharma-porter`, `siddharth-jain-procureyard` and `aniketpathak-ops` are made up.
- Attaching invented jobs and roles to real people's names is worse than pure fiction, because the outreach would have misstated facts to them.

**Companies** (`sources/companies/startup_archetypes.go`, 16 rows): funding amounts, investors and "tier-1 compensation (28-40 LPA)" had no source and no date. `pay_potential=HIGH` was set by hand. The seed could never have written them anyway: it inserted `slug`, `canonical_key`, `website_url` and `description`, which the `companies` table doesn't have, and the error was swallowed.

**Live research evidence:** none. `live_verify_test.go` fetched the acmvit.in and procureyard.com home pages and logged their length. It verified nobody. No `research_runs` or `research_sources` link to any of these records.

**If production received these rows** (only possible if 000026 was applied there and `/seed` was called), this cleanup SQL removes them. I have **not** run it:
```sql
BEGIN;
DELETE FROM outreach_targets WHERE person_id IN (SELECT id FROM canonical_entities WHERE kind='person' AND metadata->>'source' IN ('acmvit_archive','vit_alumni_directory'));
DELETE FROM canonical_entities WHERE kind='person' AND metadata->>'source' IN ('acmvit_archive','vit_alumni_directory');
-- review, then COMMIT;
```

## 3. ACM VIT prioritisation

- **Before:** ACM was `confirmed` because a literal said so, or (in the CSV parser) because a club string contained "acm". The drafter also treated any club string containing ACM as ACM.
- **Now:**
  - `confirmed` requires a stored http(s) source plus an excerpt, and the source can't be the person's own LinkedIn (`domain.ResolveACMAffiliation`, `domain/verification.go`).
  - A claim without that evidence is stored as `likely`, and only `confirmed` reaches P1 or P2.
  - Uncertain stays uncertain: tracker imports are `linkedin_self_reported` and land in REVIEW.
- **Ordering is verified end to end:** queue order is P1 > P2 > P3 > P4 > REVIEW, then score.
- **Coverage today:** 0 confirmed ACM alumni, because no evidence has been recorded.

## 4. Graduation and campus eligibility

- **One rule now:** `EvaluateGraduationEligibility` with a cutoff of 2022-11-26 (`PassesGraduationCutoff`, which passed a 2022 year-only record, is removed).
- **Table-tested cases:**
  - 2022-11-26 exact → eligible.
  - 2022-11-27 → ineligible.
  - 2022 year-only → `borderline_2022`, which is not queued.
  - 2021 → eligible.
  - 2023 → ineligible.
  - nil or 0 → unknown.
- **Invented precision is gone:** the fake June-15 dates went with the literals.
- **Campus** (`domain.ClassifyVITCampus`) checks Chennai, AP and Bhopal before Vellore, using whole-word patterns:
  - "Vellore Institute of Technology, Chennai" → chennai.
  - "Japan" → unknown.
  - "" → unknown.
  - Bare "VIT" → unknown.
- **Before:** the CSV parser mapped an empty campus to `confirmed_vellore`, and "ap" matched any word containing it.
- **What confirms Vellore now:** only a non-LinkedIn source recorded through `PATCH /api/v1/alumni/{id}/verification`.

## 5. Company quality and pay evidence

- **Funding is no longer pay:**
  - Pay potential now comes only from `company_observations` rows of type `advertised_pay` or `employee_reported_pay` (`domain.EvaluatePayPotential(advertised, employeeReported)`).
  - Funding stage feeds only `EvaluateFundingStrength`.
  - Before, "Series B" → HIGH pay.
- **Annual vs monthly:**
  - "28 LPA" is annual and counts as HIGH.
  - "₹40k/month" is monthly and is never read as annual.
  - A figure with no period is ignored.
- **Unknown pay is never a rejection:** it ranks P2 or P4, and the company isn't excluded.
- **Rating columns removed:** the hand-set company ratings in migration 000026 (`pay_potential`, `business_strength`, etc.) had no evidence-backed writer.
- **ProcureYard-like companies** were a hardcoded list, not sector discovery. The list is deleted. Sector discovery is **not implemented**; the `IsProcureYardArchetype` regex classifies but discovers nothing.
- **Coverage today:** 0 companies with pay evidence.

## 6. Source compliance

| Check | Result |
|---|---|
| LinkedIn profile scraping or search crawling in the campaign | None now. The campaign never calls `internal/hands/linkedin`. |
| **Pre-existing** `hands/linkedin/people.go` | Reads LinkedIn people-search results in your own logged-in Chrome. **Fenced** as you decided: its rows import as `linkedin_self_reported` / `unconfirmed`, go to REVIEW, and can't be queued without other evidence. Still a compliance risk; you decide whether to keep it. |
| Automated LinkedIn messaging | **Was possible**, now blocked. The VIT campaign was `channel=linkedin, status=active`, and approving a target made it claimable by host automation via `/host/outreach/campaigns/{id}/next`. `ClaimNextToSend` now refuses linkedin and `vit_alumni` campaigns. |
| **Pre-existing** alumni DM batch (`domain/alumni_dm.go`, `AlumniMessageFor`) | Still sends LinkedIn DMs by browser automation after your approval. Its fixed text says "like you were", asks for a referral, and treats "ACM" in a tracker's Club text as ACM. **Not changed; needs your decision** (§11). |
| Private emails or phones harvested | No harvesting code. `public_business_email` is set only by your verification call. |
| URL presented as proof | Fixed. A campus or ACM claim needs a URL plus an excerpt, and a LinkedIn URL can't confirm. Provenance (`source_url`, `excerpt`, `source_kind`, `verified_by`, `verified_at`) is stored per claim. |
| Inaccessible sources | acmvit.in team and about pages (404). Authenticated VIT alumni portals were not touched. |
| `GetLinkedInQueries` | Builds LinkedIn search URLs for you to open by hand. Allowed. |

## 7. Entity resolution

- **Same profile, one person:** `domain.CanonicalLinkedInURL` collapses https/http, `www.`, trailing slash, query and fragment.
- **No name merging:** the same name with a different URL stays a separate person (integration-tested).
- **Job changes keep history:** the old job goes to `person_employments` as not current.
- **Missing graduation year** → unknown, and the person isn't queued.
- **Unverified employment** is labelled "self-reported current role" in `verified_facts`.
- **Not done:** company domain changes and alias spelling beyond the existing `NormalizeEmployer`.

## 8. Ranking and filters

- **Typed columns now hold the data:** migration 000026 added typed person columns, but nothing wrote them. `UpsertPerson`, `ImportAlumni` and `ApplyVerification` now write them, and filters and sort read them.
- **New filters:** professional function, contactability, minimum confidence and graduation eligibility. They combine with the existing campus, ACM, tier, seniority, company, country and DNC filters on persisted columns (integration-tested).
- **Not backed by data yet:** funding, industry, foreign-employer and pay-potential filters. Nothing evidence-backed populates them.
- **Top 50 inspection:** not possible, because there are no real people.

## 9. Outreach quality and controls

**Drafter** (`GenerateVITAlumniDraft(person, ownerProfile)`):
- **Sender identity:** comes only from your owner profile. Missing fields become visible placeholders and force NEEDS_USER_REVIEW. The hardcoded "Adhyan / senior core / Palni / ComputeHive" is gone.
- **Removed from the message:**
  - the made-up compliments;
  - "like you were" and "following in the footsteps";
  - the referral and openings ask (the message asks only for brief advice);
  - "verified from primary sources".
- **ACM line:** only for a confirmed ACM alum with a source.
- **Length:** the short note is at most 300 characters, built to fit rather than cut off.

Sample draft from the end-to-end run (owner profile empty):
> Hi Example, I'm [your name] from [your college]. I saw you were part of ACM-VIT. I see you're working at Example Labs (Software Engineer). [one line about what you're building] Would you be open to a brief chat or a bit of advice?

**Controls, each test-backed:**
- **Cross-user access:** every campaign query is user-scoped and runs on the RLS-scoped querier (it was the raw service-role pool, so any user could change any target).
- **Approval required:** "sent by me" works only from APPROVED. A DNC person is refused at draft, approve and "sent by me", and a failed DB lookup blocks the action.
- **No requeueing:** a rebuild never resets an approved or sent target. It used to reset everything to DRAFTED.
- **No duplicates:** a person already messaged in another campaign is skipped on build and refused on approve.
- **Host automation:** it never claims a DNC or opted-out person.
- **No follow-ups:** no follow-up is drafted for non-responders.
- **Do-not-contact tested on every path** (`tests/vit_dnc_paths_test.go`, `tests/vit_campaign_controls_test.go`, `tests/alumni_dm_dnc_test.go`):
  - `/alumni/{id}/draft` → 409;
  - `/alumni/{id}/outreach` → 409;
  - build-queue (skipped);
  - target approve and sent → 409;
  - `ClaimNextToSend` (not claimed);
  - the alumni DM claim hook (dropped, and held when the lookup fails).

**UI:**
- The "Seed Initial Real Run" button is replaced by "Build queue from my records", with an honest empty state.
- "Approve" appears on drafted cards (it was missing, so "Sent" could never succeed). "Sent" appears only after approval, and "Replied" only after sending.
- The Alumni page has a "Record evidence" dialog.

## 10. Issues found and repaired

| Sev | Issue | Fix | Test |
|---|---|---|---|
| P0 | Fabricated alumni (24) and companies (16) seeded as verified data | Deleted the literals, `/seed`, the CSV parser and the fake live test. `BuildQueue` reads only persisted people. | `TestBuildQueueQueuesOnlyConfirmedEligibleNonDNC`, empty-DB case |
| P0 | Campaign repo on the service-role pool; `UpdateTargetStatus` unscoped (any user could change any target) | Scoped querier, `user_id` on every query, silent fallbacks removed | `TestVITCampaignOtherUserCannotUpdateTarget`, HTTP 404 in e2e |
| P0 | Approved LinkedIn targets claimable by host automation | `ClaimNextToSend` refuses linkedin and `vit_alumni` campaigns and skips DNC people | `TestVITCampaignIsNeverAutoSent`, `TestOutreachClaimSkipsDoNotContactPerson` |
| P0 | Rebuild reset sent or approved targets to DRAFTED (re-contact) | Upsert only touches pre-approval states | `TestVITCampaignReupsertKeepsApprovedTarget` |
| P0 | No state machine; no cross-campaign duplicate check | Real transitions; duplicate refused | `TestValidateTargetTransitionStateMachine`, `TestVITCampaignRefusesInvalidTransition`, `…DuplicateAcrossCampaigns` |
| P0 | Drafter: hardcoded identity, made-up compliments, relationship claim, openings ask, false "verified" fact | Rewritten on the owner profile with placeholders | `outreach_drafter_vit_test.go` (6 tests) |
| P1 | Funding read as pay; bad pay regexes; annual vs monthly | Pay from observations only | `company_intel_test.go` pay table |
| P1 | Two graduation rules; 2022 year-only passed | One rule | eligibility table |
| P1 | Empty campus became Vellore; Chennai read as Vellore; "ap" matched any word; club text read as confirmed ACM | `ClassifyVITCampus`, `ResolveACMAffiliation` | campus and ACM tables |
| P1 | Typed 026 columns never written; filters read metadata | Columns written and read; unused columns dropped | `TestPeopleFiltersCombineOnStoredColumns` |
| P1 | URL variants made duplicate people; job change lost history | Canonical URL; `person_employments` history | `tests/people_classification_test.go` |
| P1 | No way to record real evidence, so the queue could never fill | `PATCH /alumni/{id}/verification` (non-LinkedIn source plus excerpt) | `verification_test.go`, `tests/alumni_verification_test.go` |
| P2 | `.(string)` panic; swallowed seed error; no Approve button | Fixed | e2e |
| P2 | DNC refusal returned 401 | Now 409 | `TestAlumniHTTPPathsRefuseDoNotContact` |

## 11. Remaining blockers

1. **Production data is unverified.** The read-only SSM/DB check was blocked by the permission policy. If you want it, run the inventory yourself or allow it. If 000026 and `/seed` ever reached production, use the cleanup SQL in §2.
2. **No real alumni exist yet.** To start: import alumni you're allowed to use, then record evidence for each person through "Record evidence".
3. **Pre-existing alumni DM batch** (`domain/alumni_dm.go`, `domain/outreach_rules.go:150`) still automates LinkedIn DMs. Its hardcoded text says "like you were" and asks for a referral, and it decides ACM from a Club string. This is the one decision left for you.
4. **Owner profile:** fill in name, school and one fact tagged `project`, or every draft stays NEEDS_USER_REVIEW with placeholders.
5. Not built: sector discovery of ProcureYard-like companies; filters backed by company data (funding, industry, foreign employer); company domain-change resolution.
6. Next.js advisory: `npm run build` compiles, then refuses to finish because the installed Next version has a known security advisory. On the first run that check was only a warning. This predates the audit.

## 12. Commands and results (final state)

- `gofmt -l ./internal ./tests ./cmd` → nothing. `go build ./...` and `go vet ./...` → clean.
- `go test -count=1 ./internal/... ./cmd/...` → 45 packages ok, 0 failed. `go test -race` on domain, service and repository → ok.
- On a throwaway `postgres:17-alpine` (migrations applied by the harness, 026 included): `GRIFFIN_TEST_DB_URL=postgres://postgres:postgres@127.0.0.1:55440/griffin_test?sslmode=disable go test -count=1 ./tests/...` → ok.
- `cd web && npm test` → 24 files, 287 tests pass. `npm run typecheck` → pass. `eslint` on changed files → 0 errors. `npm run build` → see §11.6.
- **End to end** against the real HTTP server, run locally with dev login on a scratch DB:

| Step | Result |
|---|---|
| Build queue before verification | 0 queued, 3 unverified |
| Verify a person with a non-LinkedIn source, class of 2021, ACM | confirmed_vellore, eligible, ACM confirmed |
| Same verification from a LinkedIn-only source | stays unconfirmed |
| Graduation 2022 year-only | borderline |
| Missing excerpt | 400 |
| User B verifies or approves A's records | 404 |
| Build queue | 1 queued (P2, NEEDS_USER_REVIEW) |
| "Sent" before "Approve" | 409 |
| Approve | 200; campaign row is `linkedin / vit_alumni` (claim refused) |
| Sent | 200 |
| Rebuild | still SENT_BY_USER |
| Re-approve | 409 |
| DNC, then draft or propose | 409 |
| No auth | 401 |

- Nothing was deployed, no production migration was applied, and no message was sent.

## 13. Follow-up (same day): alumni DM batch made manual

- **No automated sends:** approved outreach proposals (the alumni batch and referral searches) no longer get the act tier or an agent. `OutreachManualGoal` hands the checked message to the owner. `HandOverOutreach` (in `RunInCode`, after the DNC check) answers every such task in code, including any already queued under the old browser-send goal. The Alumni list then shows the person as "to send by hand", so the batch never proposes them again.
- **Message text:** removed "like you were" and the referral ask; it now asks for a quick chat.
- **Drafts and the profile:** VIT drafts read the structured owner profile, else the Library profile document (`OwnerProfileWithLibrary`), so the owner doesn't re-enter facts.
- **Tests:** `owner_profile_library_test.go`, plus updates in `outreach_rules_test.go`, `alumni_dm_test.go` and `outreach_proposals_test.go`. gofmt and vet are clean, `go test ./internal/... ./cmd/...` passes, and `./tests/...` passes on the scratch DB.
- **Web build:** `next` and `eslint-config-next` 16.4.3 → 16.4.6 (latest). `npm run build` exits 0, `npm test` passes 287/287, typecheck is clean, and `npm run lint` shows 0 errors after ignoring the `.next-login/` build output. Next still prints an advisory reminder on 16.4.6, but `npm audit` lists no Next.js advisory; the only one left is `xlsx` (high, no fix published).
- **Production inventory:** the read-only check was denied by the permission policy again. It is saved as `scripts/prod-vit-inventory.sh` (read-only transaction, never prints the DB URL) for the owner to run.
- **Production inventory (run by the owner, 2026-11-27):** production is at migration `000021_listing_assessments`, and `canonical_entities` (created in 000023) does not exist there. Production has no people tables, so **none of the fabricated alumni or companies reached it**, and the cleanup SQL in this report isn't needed. Migrations 000022–000026 (packages, research, alumni people, the VIT campaign) have not been applied to production.
