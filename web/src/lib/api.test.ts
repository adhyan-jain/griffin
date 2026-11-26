import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { envelope, fixtures } from "@/test/fixtures";

vi.mock("@/lib/supabase", () => ({
  getSupabase: () => null,
  getSupabaseAccessToken: async () => "test-token",
}));

type Api = typeof import("@/lib/api");
let mod: Api;
const fetchMock = vi.fn();

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const lastCall = () => {
  const [url, init] = fetchMock.mock.calls.at(-1)!;
  return { url: String(url), init: init as RequestInit, body: (init as RequestInit)?.body };
};

beforeEach(async () => {
  vi.resetModules();
  vi.stubEnv("NEXT_PUBLIC_API_URL", "http://api.test");
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
  mod = await import("@/lib/api");
});
afterEach(() => vi.unstubAllEnvs());

// Shapes copied from the Go JSON (repository.CompanySummary / CompanyDetail and handlers.personView).
const companyListFixture = {
  id: "c1",
  name: "Acme",
  normalized_name: "acme",
  domain: "acme.com",
  kind: "startup",
  status: "active",
  discovered_via: "ats",
  evidence_url: null,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-02T00:00:00Z",
  open_listings: 3,
  latest_observations: [
    {
      property: "funding",
      value: "Series A, $10M",
      source_url: "https://acme.com/blog/series-a",
      claim_origin: "company_reported",
      evidence_id: null,
      observed_at: "2026-07-20T00:00:00Z",
      age_days: 120,
    },
  ],
};
const companyDetailFixture = {
  company: { ...companyListFixture, latest_observations: undefined, open_listings: undefined },
  open_listings: 1,
  listings: [
    {
      id: "l1",
      role: "SDE Intern",
      stage: "found",
      location: "Remote",
      pay: "",
      url: "https://board.example/1",
      official_url: "https://acme.com/careers/1",
      source_kind: "employer_ats",
      verification: "employer_verified",
      open: true,
      last_seen_at: "2026-11-20T00:00:00Z",
    },
  ],
  observations: [
    { ...companyListFixture.latest_observations[0], id: "o1", company_id: "c1" },
    { id: "o2", company_id: "c1", property: "advertised_pay", value: "INR 40k/mo", source_url: "https://acme.com/careers/1", claim_origin: "company_reported", evidence_id: "e1", observed_at: "2026-11-20T00:00:00Z", age_days: 6 },
  ],
  evidence: [
    { id: "e1", claim: "advertised_pay", value: "INR 40k/mo", quote: "Stipend 40k per month", source_url: "https://acme.com/careers/1", status: "supported", confidence: 90, retrieved_at: "2026-11-20T00:00:00Z" },
  ],
};
const alumniFixture = {
  id: "a1",
  name: "Alum One",
  profile_url: "https://www.linkedin.com/in/alum-one",
  company: "Acme",
  title: "Software Engineer",
  club: "ACM",
  country: "India",
  graduation_year: 2021,
  graduation_year_evidence_id: null,
  graduation_year_source: "tracker_batch",
  role_family: "tech",
  role_family_inferred: true,
  do_not_contact: false,
  contact_state: "not_contacted",
  last_verified_at: null,
  meets_cutoff: true,
  metadata: {},
};

describe("createTask", () => {
  it("serialises priority as the backend string enum and fills title/manager_domain", async () => {
    fetchMock.mockResolvedValue(json(envelope(fixtures.task)));
    const t = await mod.api.createTask({ goal: "  Plan a weekly status report ", priority: "HIGH_P1" });
    const { url, init, body } = lastCall();
    expect(url).toBe("http://api.test/api/v1/tasks");
    expect(init.method).toBe("POST");
    expect(new Headers(init.headers).get("Authorization")).toBe("Bearer test-token");
    const parsed = JSON.parse(body as string);
    expect(parsed.priority).toBe("HIGH_P1");
    expect(typeof parsed.priority).toBe("string");
    expect(parsed.manager_domain).toBe("JJ");
    expect(parsed.title).toBe("Plan a weekly status report");
    expect(parsed.input_payload).toEqual({});
    expect(t.status).toBe("COMPLETED");
  });

  it("defaults to NORMAL_P2 and truncates the title to 80 chars", async () => {
    fetchMock.mockResolvedValue(json(envelope(fixtures.task)));
    await mod.api.createTask({ goal: "x".repeat(200) });
    const parsed = JSON.parse(lastCall().body as string);
    expect(parsed.priority).toBe("NORMAL_P2");
    expect(parsed.title).toHaveLength(80);
  });
});

describe("respondApproval", () => {
  it.each([
    [true, "APPROVE"],
    [false, "REJECT"],
  ])("approve=%s sends decision %s", async (approve, decision) => {
    fetchMock.mockResolvedValue(json(envelope({})));
    await mod.api.respondApproval(fixtures.approval.id, approve, "why");
    const { url, body } = lastCall();
    expect(url).toBe(`http://api.test/api/v1/approvals/${fixtures.approval.id}/respond`);
    expect(JSON.parse(body as string)).toEqual({ decision, reason: "why" });
  });
});

describe("uploadArtifact", () => {
  it("posts multipart form data without forcing a JSON content type", async () => {
    fetchMock.mockResolvedValue(json(envelope(fixtures.artifact)));
    const file = new File(["hello"], "report.md", { type: "text/markdown" });
    const a = await mod.api.uploadArtifact(file, fixtures.task.id);
    const { url, init, body } = lastCall();
    expect(url).toBe("http://api.test/api/v1/artifacts/upload");
    expect(body).toBeInstanceOf(FormData);
    const form = body as FormData;
    expect(form.get("task_id")).toBe(fixtures.task.id);
    expect(form.get("title")).toBe("report.md");
    expect((form.get("file") as File).name).toBe("report.md");
    expect(new Headers(init.headers).has("Content-Type")).toBe(false);
    expect(a.id).toBe(fixtures.artifact.id);
  });
});

describe("presign and orchestrator", () => {
  it("returns {url, expires_at} from presign", async () => {
    fetchMock.mockResolvedValue(json(envelope(fixtures.presign)));
    expect(await mod.api.getPresignedUrl("abc")).toEqual(fixtures.presign);
  });
  it("submits request_text and returns orchestration + plan", async () => {
    fetchMock.mockResolvedValue(json(envelope(fixtures.submitResult)));
    const r = await mod.api.submitOrchestratorRequest("Plan it", "HIGH_P1");
    expect(JSON.parse(lastCall().body as string)).toEqual({ request_text: "Plan it", priority: "HIGH_P1" });
    expect(r.plan.plan_payload.child_tasks).toHaveLength(2);
    expect(r.plan.plan_payload.dependencies?.[0].depends_on_key).toBe("task_1");
  });
});

describe("list endpoints", () => {
  it.each([
    ["listTasks", () => mod.api.listTasks()],
    ["listApprovals", () => mod.api.listApprovals()],
    ["listArtifacts", () => mod.api.listArtifacts()],
    ["listManagers", () => mod.api.listManagers()],
    ["getTaskEvents", () => mod.api.getTaskEvents("x")],
    ["getTaskAttempts", () => mod.api.getTaskAttempts("x")],
  ])("%s maps a null data payload to []", async (_n, call) => {
    fetchMock.mockResolvedValue(json(envelope(null)));
    expect(await call()).toEqual([]);
  });

  it("passes status, limit and parent_id filters through the query string", async () => {
    fetchMock.mockResolvedValue(json(envelope([fixtures.task])));
    await mod.api.listTasks({ status: "RUNNING", limit: 5, parent_id: "p1" });
    const u = new URL(lastCall().url);
    expect(u.searchParams.get("status")).toBe("RUNNING");
    expect(u.searchParams.get("limit")).toBe("5");
    expect(u.searchParams.get("parent_id")).toBe("p1");
  });
  it("asks for archived tasks only when told to", async () => {
    fetchMock.mockImplementation(async () => json(envelope([fixtures.task])));
    await mod.api.listTasks({ limit: 5 });
    expect(new URL(lastCall().url).searchParams.has("archived")).toBe(false);
    await mod.api.listTasks({ archived: true });
    expect(new URL(lastCall().url).searchParams.get("archived")).toBe("true");
  });
  it("archives, restores and bulk-deletes through the task routes", async () => {
    fetchMock.mockImplementation(async () => json(envelope({ deleted: 3 })));
    await mod.api.archiveTask("t1");
    expect(lastCall().url).toContain("/api/v1/tasks/t1/archive");
    expect(lastCall().init.method).toBe("POST");
    await mod.api.unarchiveTask("t1");
    expect(lastCall().url).toContain("/api/v1/tasks/t1/unarchive");
    expect(await mod.api.deleteArchivedTasks()).toEqual({ deleted: 3 });
    expect(lastCall().url).toContain("/api/v1/tasks/archived/delete");
  });
  it("filters approvals by status", async () => {
    fetchMock.mockResolvedValue(json(envelope([fixtures.approval])));
    const r = await mod.api.listApprovals("PENDING");
    expect(lastCall().url).toContain("?status=PENDING");
    expect(r[0].status).toBe("PENDING");
  });
});

describe("error envelope parsing", () => {
  it("surfaces error.message from the Go envelope", async () => {
    fetchMock.mockResolvedValue(json(fixtures.errorEnvelope, 400));
    await expect(mod.api.getTask("x")).rejects.toThrow("goal is required");
  });
  it("falls back to HTTP status when the body is not JSON", async () => {
    fetchMock.mockResolvedValue(new Response("<html>bad gateway</html>", { status: 502, statusText: "Bad Gateway" }));
    await expect(mod.api.getTask("x")).rejects.toThrow("HTTP 502: Bad Gateway");
  });
  it("accepts a plain string error", async () => {
    fetchMock.mockResolvedValue(json({ error: "nope" }, 500));
    await expect(mod.api.getTask("x")).rejects.toThrow("nope");
  });
});

describe("getSystemStatus", () => {
  it("returns the status document", async () => {
    fetchMock.mockResolvedValue(json(envelope(fixtures.systemStatus)));
    const s = await mod.api.getSystemStatus();
    expect(lastCall().url).toBe("http://api.test/api/v1/system/status");
    expect(s.components.worker?.state).toBe("degraded");
    expect(s.components.provider?.observed_at).toBeNull();
  });
});

describe("dev login", () => {
  it("refuses when NEXT_PUBLIC_ENABLE_DEV_LOGIN is not set", async () => {
    await expect(mod.api.login()).rejects.toThrow("Dev login is disabled.");
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("sendJJMessage", () => {
  it("POSTs the message with an idempotency key and returns the conversation result", async () => {
    fetchMock.mockResolvedValue(json(envelope(fixtures.jjConversation), 201));
    const r = await mod.api.sendJJMessage({ message: "hi", conversation_id: "c1", history: [{ role: "user", text: "yo" }] });
    const { url, init, body } = lastCall();
    expect(url).toBe("http://api.test/api/v1/jj/messages");
    expect(init.method).toBe("POST");
    const parsed = JSON.parse(body as string);
    expect(parsed).toMatchObject({ message: "hi", conversation_id: "c1", history: [{ role: "user", text: "yo" }] });
    expect(typeof parsed.idempotency_key).toBe("string");
    expect(parsed.idempotency_key.length).toBeGreaterThan(8);
    expect(r.kind).toBe("conversation");
    if (r.kind === "conversation") expect(r.task_id).toBe(fixtures.jjConversation.task_id);
  });

  it("uses a fresh idempotency key per call unless one is given", async () => {
    fetchMock.mockImplementation(async () => json(envelope(fixtures.jjConversation), 201));
    await mod.api.sendJJMessage({ message: "a" });
    const k1 = JSON.parse(lastCall().body as string).idempotency_key;
    await mod.api.sendJJMessage({ message: "a" });
    const k2 = JSON.parse(lastCall().body as string).idempotency_key;
    expect(k1).not.toBe(k2);
    await mod.api.sendJJMessage({ message: "a", idempotency_key: "fixed" });
    expect(JSON.parse(lastCall().body as string).idempotency_key).toBe("fixed");
  });

  it("returns the objective result", async () => {
    fetchMock.mockResolvedValue(json(envelope(fixtures.jjObjective), 201));
    const r = await mod.api.sendJJMessage({ message: "plan a report" });
    expect(r.kind).toBe("objective");
    if (r.kind === "objective") expect(r.plan.plan_payload.child_tasks).toHaveLength(1);
  });

  it("maps 404 to JJEndpointUnavailable and does not retry", async () => {
    fetchMock.mockResolvedValue(json({ success: false, error: { message: "not found" } }, 404));
    await expect(mod.api.sendJJMessage({ message: "hi" })).rejects.toBeInstanceOf(mod.JJEndpointUnavailable);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("keeps other errors as ordinary errors", async () => {
    fetchMock.mockResolvedValue(json({ success: false, error: { message: "boom" } }, 500));
    const err = await mod.api.sendJJMessage({ message: "hi" }).catch((e) => e);
    expect(err).not.toBeInstanceOf(mod.JJEndpointUnavailable);
    expect(err.message).toBe("boom");
  });
});

describe("Research & Intelligence API", () => {
  // Fixtures copy the JSON field names of domain.ResearchRun / ResearchFinding / EvidenceItem.
  const runFixture = {
    id: "run-1",
    user_id: "u-1",
    playbook: "generic",
    objective: "Investigate AI",
    depth: "standard",
    status: "COMPLETED",
    plan: { questions: [{ id: "q1", question: "Who?", status: "unresolved", finding_ids: [] }], initial_queries: ["ai"] },
    context_data: {},
    constraints_list: [],
    budget_config: { max_searches: 6, max_pages: 10, max_model_calls: 12, max_time_seconds: 120 },
    progress_metrics: { searches_done: 6, pages_inspected: 4, model_calls: 7, findings_count: 1, time_ms: 31000 },
    findings: [
      {
        id: "f1", topic: "funding", claim: "Raised $5M", extracted_value: "$5M", quote: "raised $5M in seed",
        evidence_id: "ev-1", source_url: "https://a.example/x", confidence: 80, status: "supported",
        subject: "Acme", property: "funding",
      },
    ],
    unresolved_questions: ["Who?"],
    report_markdown: "# Research report",
    report_structured: { stop_reason: "budget_searches", complete: false, open_questions: ["Who?"], conflicts: [] },
    round: 3,
    pending_queries: [],
    executed_queries: ["ai"],
    pending_urls: [],
    seen_urls: [],
    stop_reason: "budget_searches",
    next_step_index: 9,
    revision: 12,
    created_at: "2026-11-20T10:00:00Z",
    updated_at: "2026-11-20T10:05:00Z",
    completed_at: "2026-11-20T10:05:00Z",
  };
  const evidenceFixture = {
    id: "ev-1", user_id: "u-1", run_id: "run-1", claim: "Raised $5M", extracted_value: "$5M",
    excerpt_quote: "raised $5M in seed", source_url: "https://a.example/x", source_domain: "a.example",
    source_type: "credible_publication", retrieved_at: "2026-11-20T10:02:00Z", reliability: "secondary_credible",
    confidence: 80, status: "supported", extraction_method: "model", created_at: "2026-11-20T10:02:00Z",
  };

  it("creates a research run with the Go request field names", async () => {
    fetchMock.mockResolvedValue(json(envelope(runFixture), 202));
    const run = await mod.api.createResearchRun({
      objective: "Investigate AI",
      depth: "standard",
      playbook: "company_intelligence",
      research_questions: ["Who funds it?"],
    });
    const { url, init, body } = lastCall();
    expect(url).toBe("http://api.test/api/v1/research");
    expect(init.method).toBe("POST");
    const parsed = JSON.parse(body as string);
    expect(parsed).toEqual({
      objective: "Investigate AI",
      depth: "standard",
      playbook: "company_intelligence",
      research_questions: ["Who funds it?"],
    });
    expect(parsed).not.toHaveProperty("questions");
    expect(run.id).toBe("run-1");
    expect(run.stop_reason).toBe("budget_searches");
    expect(run.created_at).toBe("2026-11-20T10:00:00Z");
    expect(run.findings?.[0].quote).toBe("raised $5M in seed");
  });

  it("lists research runs and gets evidence", async () => {
    fetchMock.mockResolvedValue(json(envelope([runFixture])));
    const list = await mod.api.listResearchRuns();
    expect(lastCall().url).toBe("http://api.test/api/v1/research");
    expect(list).toHaveLength(1);
    expect(list[0].round).toBe(3);

    fetchMock.mockResolvedValue(json(envelope([evidenceFixture])));
    const ev = await mod.api.getResearchEvidence("run-1");
    expect(lastCall().url).toBe("http://api.test/api/v1/research/run-1/evidence");
    expect(ev).toHaveLength(1);
    expect(ev[0].excerpt_quote).toBe("raised $5M in seed");
    expect(ev[0].retrieved_at).toBe("2026-11-20T10:02:00Z");
    expect(ev[0].status).toBe("supported");
  });

  it("pauses, resumes and cancels research runs", async () => {
    fetchMock.mockResolvedValue(json(envelope({ status: "paused" })));
    await mod.api.pauseResearchRun("run-1");
    expect(lastCall().url).toBe("http://api.test/api/v1/research/run-1/pause");
    expect(lastCall().init.method).toBe("POST");

    fetchMock.mockResolvedValue(json(envelope({ ...runFixture, status: "INFORMATION_RETRIEVAL" })));
    await mod.api.resumeResearchRun("run-1");
    expect(lastCall().url).toBe("http://api.test/api/v1/research/run-1/resume");

    fetchMock.mockResolvedValue(json(envelope({ status: "cancelled" })));
    const res = await mod.api.cancelResearchRun("run-1");
    expect(lastCall().url).toBe("http://api.test/api/v1/research/run-1/cancel");
    expect(lastCall().init.method).toBe("POST");
    expect(res.status).toBe("cancelled");
  });

  it("lists companies from /companies with the Go list shape", async () => {
    fetchMock.mockResolvedValue(json(envelope([companyListFixture])));
    const list = await mod.api.listCompanies({ limit: 50 });
    expect(lastCall().url).toBe("http://api.test/api/v1/companies?limit=50");
    expect(list[0].open_listings).toBe(3);
    expect(list[0].latest_observations[0]).toMatchObject({ property: "funding", claim_origin: "company_reported", age_days: 120 });

    fetchMock.mockResolvedValue(json(envelope(null)));
    expect(await mod.api.listCompanies()).toEqual([]);
    expect(lastCall().url).toBe("http://api.test/api/v1/companies");
  });

  it("gets company detail with listings, observation history and evidence", async () => {
    fetchMock.mockResolvedValue(json(envelope(companyDetailFixture)));
    const d = await mod.api.getCompany("c1");
    expect(lastCall().url).toBe("http://api.test/api/v1/companies/c1");
    expect(d.listings[0].official_url).toBe("https://acme.com/careers/1");
    expect(d.observations).toHaveLength(2);
    expect(d.evidence[0].status).toBe("supported");
  });

  it("maps alumni filters to server query params and sends none client-side", async () => {
    fetchMock.mockResolvedValue(json(envelope([alumniFixture])));
    const list = await mod.api.listAlumni({
      grad_year_max: 2022,
      role_family: "tech",
      company: " Acme ",
      country: "India",
      graduation_eligibility: "eligible",
      professional_function: "technology",
      contactability_status: "public_contact_available",
      min_classification_confidence: 60,
      do_not_contact: false,
      limit: 200,
    });
    const u = new URL(lastCall().url);
    expect(u.pathname).toBe("/api/v1/alumni");
    expect(Object.fromEntries(u.searchParams)).toEqual({
      grad_year_max: "2022",
      role_family: "tech",
      company: "Acme",
      country: "India",
      graduation_eligibility: "eligible",
      professional_function: "technology",
      contactability_status: "public_contact_available",
      min_classification_confidence: "60",
      do_not_contact: "false",
      limit: "200",
    });
    expect(list[0]).toMatchObject({ meets_cutoff: true, graduation_year_source: "tracker_batch", role_family_inferred: true });

    fetchMock.mockResolvedValue(json(envelope([])));
    await mod.api.listAlumni();
    expect(lastCall().url).toBe("http://api.test/api/v1/alumni");
  });

  it("PATCHes contact state and do-not-contact", async () => {
    fetchMock.mockResolvedValue(json(envelope({ ...alumniFixture, contact_state: "opted_out", do_not_contact: true })));
    const p = await mod.api.updateAlumnus("a1", { contact_state: "opted_out" });
    expect(lastCall().url).toBe("http://api.test/api/v1/alumni/a1");
    expect(lastCall().init.method).toBe("PATCH");
    expect(JSON.parse(lastCall().body as string)).toEqual({ contact_state: "opted_out" });
    expect(p.do_not_contact).toBe(true);
  });

  it("PATCHes owner verification evidence", async () => {
    fetchMock.mockResolvedValue(json(envelope({ ...alumniFixture, vit_affiliation: "confirmed_vellore" })));
    const body = { campus: { value: "vellore" as const, source_url: "https://vit.ac.in/a", excerpt: "VIT Vellore" } };
    const p = await mod.api.verifyAlumnus("a1", body);
    expect(lastCall().url).toBe("http://api.test/api/v1/alumni/a1/verification");
    expect(lastCall().init.method).toBe("PATCH");
    expect(JSON.parse(lastCall().body as string)).toEqual(body);
    expect(p.vit_affiliation).toBe("confirmed_vellore");
  });

  it("builds the VIT queue with POST build-queue", async () => {
    fetchMock.mockResolvedValue(json(envelope({ considered: 0, queued: 0, needs_user_review: 0, skipped_do_not_contact: 0, skipped_excluded_employer: 0, skipped_unverified: 0, skipped_no_profile_url: 0, stats: { total_people: 0 } })));
    const res = await mod.api.buildVITQueue();
    expect(lastCall().url).toBe("http://api.test/api/v1/campaigns/vit-alumni/build-queue");
    expect(lastCall().init?.method).toBe("POST");
    expect(res.queued).toBe(0);
  });

  it("imports alumni and returns the counts", async () => {
    fetchMock.mockResolvedValue(json(envelope({ created: 4, updated: 2, skipped: 1 })));
    const r = await mod.api.importAlumni();
    expect(lastCall().url).toBe("http://api.test/api/v1/alumni/import");
    expect(lastCall().init.method).toBe("POST");
    expect(r).toEqual({ created: 4, updated: 2, skipped: 1 });
  });

  it("files outreach as {approval, person} and surfaces a 409 as ApiError", async () => {
    fetchMock.mockResolvedValue(
      json(envelope({ approval: { id: "app-1", action_type: "alumni_outreach" }, person: { ...alumniFixture, contact_state: "drafted" } }), 201),
    );
    const res = await mod.api.proposeAlumniOutreach("a1", { draft_text: "Hi", channel: "linkedin" });
    expect(lastCall().url).toBe("http://api.test/api/v1/alumni/a1/outreach");
    expect(JSON.parse(lastCall().body as string)).toEqual({ draft_text: "Hi", channel: "linkedin" });
    expect(res.approval.id).toBe("app-1");
    expect(res.person.contact_state).toBe("drafted");

    fetchMock.mockResolvedValue(json({ success: false, error: { message: "conflict: Alum is marked do not contact" } }, 409));
    await expect(mod.api.proposeAlumniOutreach("a1", { draft_text: "Hi", channel: "email" })).rejects.toMatchObject({
      status: 409,
    });
  });
});
