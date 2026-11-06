// Measures how many API requests the built PWA makes while one task detail page is open.
// The API is mocked at the network layer (40 ms latency, roughly a warm Lambda), so the numbers
// are the frontend's own behaviour. Regression tool for the 2026-11-03 request-storm incident.
//
// Prereq: a production build served on BASE, built with NEXT_PUBLIC_ENABLE_DEV_LOGIN=true and
// NEXT_PUBLIC_API_URL=http://localhost:9999 (scripts/measure-polling.sh does this).
// Usage: node scripts/measure-polling.mjs [--seconds 60] [--scenario active|completes|failing|hidden|chat|chat-failed|chat-404|chat-objective] [--shots DIR]
//        [--base http://localhost:3300] [--budget 150]
import { chromium } from "playwright-core";

const arg = (name, dflt) => {
  const i = process.argv.indexOf(`--${name}`);
  return i > -1 ? process.argv[i + 1] : dflt;
};
const SCENARIO = arg("scenario", "active");
// chat, chat-failed, chat-404, chat-objective drive /chat (type a message, press Send) instead of /tasks/:id.
const CHAT = SCENARIO.startsWith("chat");
const SECONDS = Number(arg("seconds", CHAT ? 16 : 60));
const SHOTS = arg("shots", ""); // directory for 390px screenshots (chat scenarios)
const BASE = arg("base", process.argv.includes("--prod") ? "https://griffin-pwa.vercel.app" : "http://localhost:3300");
const BUDGET = Number(arg("budget", 150)); // max total API requests allowed in the window
// --prod measures the DEPLOYED bundle (base defaults to the production PWA). Every API request is
// still intercepted and answered by the mock below, so no request ever reaches the real API, and
// a fake Supabase session is injected so the app treats the browser as signed in. No credentials.
const PROD = process.argv.includes("--prod");
const API = PROD ? "https://fq322jvjt4.execute-api.ap-south-1.amazonaws.com" : "http://localhost:9999";
const SUPABASE_REF = "dtsrkpuohnxorxaqeqwr"; // from the public NEXT_PUBLIC_SUPABASE_URL in the bundle
const LATENCY_MS = 40;

const ROOT = "6947c6e0-0000-4000-8000-000000000001";
const C1 = "77c05996-0000-4000-8000-000000000002";
const C2 = "18505a74-0000-4000-8000-000000000003";
const T0 = new Date().toISOString();
const t = Date.now();
const elapsed = () => (Date.now() - t) / 1000;
// terminal / terminal-partial: the task is ALREADY finished when the page opens and the mock holds
// real attempts and events. The page must show them (the 2026-11-04 "Activity 0, Attempts 0" bug).
const TERMINAL = SCENARIO === "terminal" || SCENARIO === "terminal-partial";
const completed = () => TERMINAL || (SCENARIO === "completes" && elapsed() > 20);

const task = (id, over = {}) => ({
  id, user_id: "u1", manager_domain: "research", title: `task ${id.slice(0, 8)}`, goal: "goal",
  status: "RUNNING", priority: "NORMAL_P2", retry_count: 0, max_retries: 3, created_at: T0, updated_at: new Date().toISOString(),
  input_payload: {}, ...over,
});
const root = () => task(ROOT, { manager_domain: "JJ", status: completed() ? "COMPLETED" : "RUNNING", leased_by: "JJ_ORCHESTRATOR", max_retries: 0 });
const kids = () =>
  SCENARIO === "chat-objective"
    ? [
        task(C1, { parent_id: ROOT, status: "COMPLETED", title: "Collect updates", output_payload: { response: "Shipped the login fix.\n\n- API: 3 PRs merged\n- Web: 2 PRs merged\n- Infra: no changes\n- Open risk: worker cold starts" } }),
        task(C2, { parent_id: ROOT, status: "COMPLETED", title: "Send report", output_payload: {} }),
      ]
    : [
        task(C1, { parent_id: ROOT, status: completed() ? "COMPLETED" : "RUNNING", leased_by: completed() ? null : "griffin-worker-1" }),
        task(C2, { parent_id: ROOT, status: completed() ? "COMPLETED" : "WAITING_FOR_DEPENDENCY" }),
      ];

// Chat mocks: POST /jj/messages starts a reply task that finishes (or fails) a few seconds later.
const CT = "c0ffee00-0000-4000-8000-000000000101";
let msgAt = null;
const sinceMsg = () => (msgAt === null ? 0 : (Date.now() - msgAt) / 1000);
const REPLY_AFTER_S = 6;
const FAIL_AFTER_S = 3;
const replyTask = () => {
  const s = sinceMsg();
  if (SCENARIO === "chat-failed") return task(CT, { manager_domain: "JJ", status: s > FAIL_AFTER_S ? "FAILED" : "RUNNING", failure_reason: s > FAIL_AFTER_S ? "provider timeout after 30s" : null });
  return task(CT, {
    manager_domain: "JJ",
    status: s > REPLY_AFTER_S ? "COMPLETED" : s > 2 ? "RUNNING" : "QUEUED",
    output_payload: s > REPLY_AFTER_S ? { response: "You have two things today:\n\n- Review the weekly report\n- Ship the login fix", provider: { name: "bedrock", version: "1", model: "claude-sonnet" } } : {},
  });
};
const submitResult = () => ({
  orchestration: { id: "d1", root_task_id: ROOT, request_text: "x", manager_id: "JJ", status: "DELEGATED", created_at: T0 },
  plan: { id: "e1", objective: "Weekly status report", manager_id: "JJ", status: "VALIDATED", plan_payload: { objective: "Weekly status report", manager_id: "JJ", child_tasks: [{ key: "task_1", title: "Collect updates", manager_domain: "RESEARCH", approval_required: false }, { key: "task_2", title: "Send report", manager_domain: "COMMS", approval_required: false }], dependencies: [] } },
});

function respond(url, method) {
  const u = new URL(url);
  const p = u.pathname;
  if (CHAT && p === "/api/v1/jj/messages" && method === "POST") {
    if (SCENARIO === "chat-404") return { status: 404, body: { success: false, error: { code: "NOT_FOUND", message: "no such route" } } };
    msgAt = Date.now();
    const conv = "c0ffee00-0000-4000-8000-0000000000c1";
    if (SCENARIO === "chat-objective") return { status: 201, body: { success: true, data: { kind: "objective", conversation_id: conv, ...submitResult(), runner: { wake: "signaled" } } } };
    return { status: 201, body: { success: true, data: { kind: "conversation", conversation_id: conv, task_id: CT, runner: SCENARIO === "chat-wake-failed" ? { wake: "failed", detail: "ecs run-task throttled" } : { wake: "signaled" } } } };
  }
  if (CHAT && p === "/api/v1/orchestrator/requests" && method === "POST") return { status: 201, body: { success: true, data: submitResult() } };
  if (CHAT && p === `/api/v1/tasks/${CT}`) return { status: 200, body: { success: true, data: replyTask() } };
  if (SCENARIO === "failing" && p !== "/api/v1/runner/status") return { status: 503, body: { success: false, error: { code: "UNAVAILABLE", message: "down" } } };
  const ok = (data) => ({ status: 200, body: { success: true, data } });
  if (p === `/api/v1/tasks/${ROOT}`) return ok(root());
  if (/^\/api\/v1\/tasks\/[0-9a-f-]{36}$/.test(p)) return ok(kids().find((k) => p.endsWith(k.id)) ?? root());
  if (p === "/api/v1/tasks") return ok(u.searchParams.get("parent_id") === ROOT ? kids() : [root(), ...kids()]);
  if (p.endsWith("/attempts"))
    return ok(
      TERMINAL
        ? [{ id: "a1", task_id: ROOT, worker_id: "griffin-worker-1", attempt_number: 1, status: "COMPLETED", prompt_tokens: 5000, completion_tokens: 2029, duration_ms: 13891, started_at: T0, completed_at: T0 }]
        : [],
    );
  if (p.endsWith("/events")) {
    const ev = (id, msg) => ({ id, task_id: ROOT, event_type: "STATUS_CHANGE", message: msg, created_at: T0 });
    return ok(TERMINAL ? [ev(1, "created"), ev(2, "unblocked"), ev(3, "claimed by worker griffin-worker-1"), ev(4, "completed")] : [ev(1, "created")]);
  }
  if (p === "/api/v1/artifacts") {
    if (SCENARIO === "terminal-partial") return { status: 500, body: { success: false, error: { code: "INTERNAL_ERROR", message: "boom" } } };
    return ok([]);
  }
  if (p === "/api/v1/approvals") return ok([]);
  if (p === "/api/v1/runner/status") return ok({ state: "running", scale_to_zero_enabled: true, timestamp: T0 });
  if (p === "/api/v1/managers") return ok([]);
  if (p === "/api/v1/system/status") {
    const c = (state) => ({ state, evidence: "mock", observed_at: T0 });
    return ok({ generated_at: T0, components: { api: c("ok"), database: c("ok"), worker: c("ok"), provider: c("ok"), scheduler: c("unknown"), storage: c("ok"), runner: c("ok") } });
  }
  return { status: 404, body: { success: false, error: { code: "NOT_FOUND", message: "no such route" } } };
}

const normalize = (url) => new URL(url).pathname.replace(/[0-9a-f]{8}-[0-9a-f-]{27}/g, ":id") + (new URL(url).searchParams.has("parent_id") ? "?parent_id" : "");

const browser = await chromium.launch({ executablePath: "/usr/bin/chromium", args: ["--no-sandbox"] });
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 } });
await ctx.addInitScript(({ prod, ref }) => {
  localStorage.setItem("griffin_auth_token", "mock.jwt.token"); // honoured by dev-login builds only
  localStorage.setItem("griffin_user_profile", JSON.stringify({ id: "u1", email: "operator@example.com", role: "OPERATOR" }));
  if (prod) {
    // supabase-js reads its session from localStorage; a far-future expiry avoids any refresh call.
    const now = Math.floor(Date.now() / 1000);
    localStorage.setItem(
      `sb-${ref}-auth-token`,
      JSON.stringify({
        access_token: "mock.jwt.token", refresh_token: "mock-refresh", token_type: "bearer", expires_in: 86400, expires_at: now + 86400,
        user: { id: "u1", aud: "authenticated", role: "authenticated", email: "operator@example.com", app_metadata: {}, user_metadata: {}, created_at: "2026-01-01T00:00:00Z" },
      }),
    );
  }
}, { prod: PROD, ref: SUPABASE_REF });
const page = await ctx.newPage();

const calls = []; // { at, key }
const inflight = new Map();
let maxIdentical = 0;
page.on("request", (r) => {
  if (!r.url().startsWith(API) || r.method() === "OPTIONS") return;
  const key = `${r.method()} ${r.url()}`;
  calls.push({ at: elapsed(), key: `${r.method()} ${normalize(r.url())}` });
  const n = (inflight.get(key) ?? 0) + 1;
  inflight.set(key, n);
  maxIdentical = Math.max(maxIdentical, n);
});
const done = (r) => {
  const key = `${r.method()} ${r.url()}`;
  if (inflight.has(key)) inflight.set(key, Math.max(0, inflight.get(key) - 1));
};
page.on("requestfinished", done);
page.on("requestfailed", done);

await page.route(`${API}/**`, async (route) => {
  const req = route.request();
  // In the terminal scenarios the summary answers fast and the detail endpoints slowly, so the task
  // flips to terminal while attempts/events are still in flight (the window the bug lived in).
  const slowDetail = TERMINAL && /\/(attempts|events)$|\/artifacts$/.test(new URL(req.url()).pathname);
  await new Promise((r) => setTimeout(r, TERMINAL ? (slowDetail ? 200 : 15) : LATENCY_MS));
  const { status, body } = respond(req.url(), req.method());
  await route.fulfill({ status, contentType: "application/json", headers: { "access-control-allow-origin": BASE }, body: JSON.stringify(body) });
});

const shot = async (name) => {
  if (SHOTS) await page.screenshot({ path: `${SHOTS}/${name}.png` });
};
const chatChecks = []; // { ok, msg } assertions about what the user sees
const check = (ok, msg) => chatChecks.push({ ok, msg });
let sendAt = 0;

if (CHAT) {
  await page.goto(`${BASE}/chat`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("Message JJ").waitFor();
  await shot(`${SCENARIO}-0-empty`);
  await page.getByLabel("Message JJ").fill("What is on my plate today?");
  sendAt = elapsed();
  await page.getByRole("button", { name: "Send" }).click();
} else {
  await page.goto(`${BASE}/tasks/${ROOT}`, { waitUntil: "domcontentloaded" });
}
if (SCENARIO === "hidden") {
  await page.waitForTimeout(5000);
  // Emulate a background tab: the page reports hidden and fires visibilitychange.
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "hidden" });
    document.dispatchEvent(new Event("visibilitychange"));
  });
}
const startedAt = elapsed();
if (CHAT) {
  const visible = (locator, ms = 1500) => locator.waitFor({ timeout: ms }).then(() => true, () => false);
  if (SCENARIO === "chat" || SCENARIO === "chat-failed") {
    check(await visible(page.getByText("JJ is thinking")), "'JJ is thinking' is shown while waiting");
    await shot(`${SCENARIO}-1-thinking`);
  }
  if (SCENARIO === "chat") {
    check(!(await visible(page.getByTestId("jj-reply"), 3000)), "no reply is shown before the task completes");
    check(await visible(page.getByText("Review the weekly report"), (REPLY_AFTER_S + 6) * 1000), "reply text appears in the DOM");
    check((await page.getByTestId("jj-reply").count()) === 1, "exactly one assistant reply");
    check(await visible(page.getByText("bedrock")), "provider footnote is shown");
    await shot("chat-2-reply");
  } else if (SCENARIO === "chat-failed") {
    check(await visible(page.getByText("provider timeout after 30s"), (FAIL_AFTER_S + 6) * 1000), "failure reason is shown");
    check(await visible(page.getByRole("button", { name: "Retry" })), "Retry button is shown");
    await shot("chat-failed-3-failed");
  } else if (SCENARIO === "chat-404") {
    check(await visible(page.getByTestId("plan-only-label"), 5000), "fallback is labelled 'Plan only'");
    check(await visible(page.getByTestId("plan-card")), "plan card is shown");
    await shot("chat-404-plan-only");
  } else if (SCENARIO === "chat-objective") {
    check(await visible(page.getByTestId("plan-result"), 12000), "Result section appears when all steps finish");
    check(await visible(page.getByText("This step finished without a result")), "empty output says 'finished without a result'");
    check(await visible(page.getByText("Shipped the login fix")), "completed step text is shown");
    await shot("chat-objective-result");
  }
  const rest = SECONDS * 1000 - (elapsed() - startedAt) * 1000;
  if (rest > 0) await page.waitForTimeout(rest);
} else {
  await page.waitForTimeout(SECONDS * 1000);
}
const endedAt = elapsed();
const tabLabels = TERMINAL ? await page.locator('[role="tab"]').allInnerTexts() : [];
await browser.close();

const windowCalls = calls.filter((c) => c.at >= startedAt && c.at <= endedAt);
const secs = endedAt - startedAt;
const byEndpoint = {};
for (const c of windowCalls) byEndpoint[c.key] = (byEndpoint[c.key] ?? 0) + 1;
// Only the task page's own endpoints must go quiet; the app shell keeps polling runner/system/approvals on purpose.
const TASK_ENDPOINT = /^GET \/api\/v1\/(tasks|artifacts)/;
const after = completed() ? windowCalls.filter((c) => c.at > 26 && TASK_ENDPOINT.test(c.key)) : [];

console.log(`scenario=${SCENARIO} window=${secs.toFixed(1)}s`);
console.log(`total API requests: ${windowCalls.length}  (${(windowCalls.length / secs).toFixed(2)} req/s)`);
console.log(`max identical requests in flight at once: ${maxIdentical}`);
console.log("per endpoint:");
for (const [k, v] of Object.entries(byEndpoint).sort((a, b) => b[1] - a[1])) console.log(`  ${String(v).padStart(6)}  ${k}`);
if (SCENARIO === "completes") console.log(`task requests after the task reached a terminal state (t>26s): ${after.length}`);

let failed = false;
if (TERMINAL) {
  const clean = tabLabels.map((s) => s.replace(/\s+/g, " ").trim());
  console.log(`tab labels: ${clean.join(" | ")}`);
  for (const want of ["Activity 4", "Attempts 1"]) {
    if (!clean.includes(want)) { console.error(`FAIL: the page did not show "${want}" although the API returned that data`); failed = true; }
  }
}
if (windowCalls.length > BUDGET) { console.error(`FAIL: ${windowCalls.length} requests exceeds budget ${BUDGET}`); failed = true; }
// Terminal scenarios deliberately abort the stopped poller's requests and re-issue them once; the
// aborted request stays "in flight" in this harness until the mock's timer fires, so skip the check.
if (maxIdentical > 1 && !TERMINAL) { console.error(`FAIL: ${maxIdentical} identical requests were in flight at once`); failed = true; }
if (SCENARIO === "completes" && after.length > 2) { console.error(`FAIL: task polling continued after a terminal state (${after.length} requests)`); failed = true; }
if (CHAT) {
  const chatCalls = calls.filter((c) => c.at >= sendAt - 0.5);
  const n = (key, from = 0) => chatCalls.filter((c) => c.key === key && c.at >= from).length;
  const rate = chatCalls.length / (endedAt - sendAt);
  console.log(`chat requests since send: ${chatCalls.length} (${rate.toFixed(2)} req/s)`);
  const terminalS = SCENARIO === "chat-failed" ? FAIL_AFTER_S : REPLY_AFTER_S;
  const lateTaskPolls = n("GET /api/v1/tasks/:id", sendAt + terminalS + 4);
  if (SCENARIO === "chat" || SCENARIO === "chat-failed") console.log(`reply-task polls after it finished (+4s grace): ${lateTaskPolls}`);
  check(rate <= 1.5, `request rate ${rate.toFixed(2)} req/s <= 1.5`);
  check(n("POST /api/v1/jj/messages") === 1, "exactly one POST /jj/messages (no retry storm)");
  if (SCENARIO === "chat" || SCENARIO === "chat-failed") check(lateTaskPolls <= 1, "polling stopped after the terminal state");
  if (SCENARIO === "chat-404") check(n("POST /api/v1/orchestrator/requests") === 1, "exactly one fallback POST /orchestrator/requests");
  for (const c of chatChecks) {
    console.log(`${c.ok ? "ok  " : "FAIL"} ${c.msg}`);
    if (!c.ok) failed = true;
  }
}
if (!failed) console.log("PASS");
process.exit(failed ? 1 : 0);
