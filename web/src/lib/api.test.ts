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
