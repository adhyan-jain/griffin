// Measures how many API requests the built PWA makes while one task detail page is open.
// The API is mocked at the network layer (40 ms latency, roughly a warm Lambda), so the numbers
// are the frontend's own behaviour. Regression tool for the 2026-11-03 request-storm incident.
//
// Prereq: a production build served on BASE, built with NEXT_PUBLIC_ENABLE_DEV_LOGIN=true and
// NEXT_PUBLIC_API_URL=http://localhost:9999 (scripts/measure-polling.sh does this).
// Usage: node scripts/measure-polling.mjs [--seconds 60] [--scenario active|completes|failing|hidden]
//        [--base http://localhost:3300] [--budget 150]
import { chromium } from "playwright-core";

const arg = (name, dflt) => {
  const i = process.argv.indexOf(`--${name}`);
  return i > -1 ? process.argv[i + 1] : dflt;
};
const SECONDS = Number(arg("seconds", 60));
const SCENARIO = arg("scenario", "active");
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
const completed = () => SCENARIO === "completes" && elapsed() > 20;

const task = (id, over = {}) => ({
  id, user_id: "u1", manager_domain: "research", title: `task ${id.slice(0, 8)}`, goal: "goal",
  status: "RUNNING", priority: "NORMAL_P2", retry_count: 0, max_retries: 3, created_at: T0, updated_at: new Date().toISOString(),
  input_payload: {}, ...over,
});
const root = () => task(ROOT, { manager_domain: "JJ", status: completed() ? "COMPLETED" : "RUNNING", leased_by: "JJ_ORCHESTRATOR", max_retries: 0 });
const kids = () => [
  task(C1, { parent_id: ROOT, status: completed() ? "COMPLETED" : "RUNNING", leased_by: completed() ? null : "griffin-worker-1" }),
  task(C2, { parent_id: ROOT, status: completed() ? "COMPLETED" : "WAITING_FOR_DEPENDENCY" }),
];

function respond(url, method) {
  const u = new URL(url);
  const p = u.pathname;
  if (SCENARIO === "failing" && p !== "/api/v1/runner/status") return { status: 503, body: { success: false, error: { code: "UNAVAILABLE", message: "down" } } };
  const ok = (data) => ({ status: 200, body: { success: true, data } });
  if (p === `/api/v1/tasks/${ROOT}`) return ok(root());
  if (/^\/api\/v1\/tasks\/[0-9a-f-]{36}$/.test(p)) return ok(kids().find((k) => p.endsWith(k.id)) ?? root());
  if (p === "/api/v1/tasks") return ok(u.searchParams.get("parent_id") === ROOT ? kids() : [root(), ...kids()]);
  if (p.endsWith("/attempts")) return ok([]);
  if (p.endsWith("/events")) return ok([{ id: 1, task_id: ROOT, event_type: "STATUS_CHANGE", message: "created", created_at: T0 }]);
  if (p === "/api/v1/artifacts") return ok([]);
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
  await new Promise((r) => setTimeout(r, LATENCY_MS));
  const { status, body } = respond(req.url(), req.method());
  await route.fulfill({ status, contentType: "application/json", headers: { "access-control-allow-origin": BASE }, body: JSON.stringify(body) });
});

await page.goto(`${BASE}/tasks/${ROOT}`, { waitUntil: "domcontentloaded" });
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
await page.waitForTimeout(SECONDS * 1000);
const endedAt = elapsed();
await browser.close();

const windowCalls = calls.filter((c) => c.at >= startedAt && c.at <= endedAt);
const secs = endedAt - startedAt;
const byEndpoint = {};
for (const c of windowCalls) byEndpoint[c.key] = (byEndpoint[c.key] ?? 0) + 1;
const after = completed() ? windowCalls.filter((c) => c.at > 26) : [];

console.log(`scenario=${SCENARIO} window=${secs.toFixed(1)}s`);
console.log(`total API requests: ${windowCalls.length}  (${(windowCalls.length / secs).toFixed(2)} req/s)`);
console.log(`max identical requests in flight at once: ${maxIdentical}`);
console.log("per endpoint:");
for (const [k, v] of Object.entries(byEndpoint).sort((a, b) => b[1] - a[1])) console.log(`  ${String(v).padStart(6)}  ${k}`);
if (SCENARIO === "completes") console.log(`requests after the task reached a terminal state (t>26s): ${after.length}`);

let failed = false;
if (windowCalls.length > BUDGET) { console.error(`FAIL: ${windowCalls.length} requests exceeds budget ${BUDGET}`); failed = true; }
if (maxIdentical > 1) { console.error(`FAIL: ${maxIdentical} identical requests were in flight at once`); failed = true; }
if (SCENARIO === "completes" && after.length > 4) { console.error(`FAIL: polling continued after a terminal state (${after.length} requests)`); failed = true; }
if (!failed) console.log("PASS");
process.exit(failed ? 1 : 0);
