// UI check for the task list and detail page: layout (no stray scrollbars, runner chip), the done
// filter windows, and delete / resume / run-again incl. bulk selection. The API is a stateful mock
// at the network layer. Prereq: a build served on --base (scripts/ui-tasks.sh does it).
// Usage: node scripts/ui-tasks.mjs [--base http://localhost:3301] [--shots DIR]
import { chromium } from "playwright-core";

const arg = (n, d) => {
  const i = process.argv.indexOf(`--${n}`);
  return i > -1 ? process.argv[i + 1] : d;
};
const BASE = arg("base", "http://localhost:3301");
const SHOTS = arg("shots", "");
const API = "http://localhost:9999";
const DAY = 86_400_000;
const ago = (d) => new Date(Date.now() - d * DAY).toISOString();
const FINISHED = ["COMPLETED", "FAILED", "CANCELLED"];

const mk = (id, title, status, daysAgo, over = {}) => ({
  id, user_id: "u1", manager_domain: "research", title, goal: `goal of ${title}`, status, priority: "NORMAL_P2",
  retry_count: 0, max_retries: 3, created_at: ago(daysAgo + 0.1), updated_at: ago(daysAgo),
  completed_at: FINISHED.includes(status) ? ago(daysAgo) : undefined, input_payload: {}, ...over,
});
const PLAN = "a0000000-0000-4000-8000-000000000007";
let tasks = [
  mk("a0000000-0000-4000-8000-000000000001", "Running now", "RUNNING", 0),
  mk("a0000000-0000-4000-8000-000000000002", "Done two days ago", "COMPLETED", 2),
  mk("a0000000-0000-4000-8000-000000000003", "Done forty days ago", "COMPLETED", 40),
  mk("a0000000-0000-4000-8000-000000000004", "Done hundred days ago", "COMPLETED", 100),
  mk("a0000000-0000-4000-8000-000000000005", "Failed standalone", "FAILED", 1, { failure_reason: "boom" }),
  mk("a0000000-0000-4000-8000-000000000006", "Cancelled standalone", "CANCELLED", 3),
  mk(PLAN, "JJ Orchestration: weekly report", "FAILED", 5, { manager_domain: "JJ", goal: "weekly report" }),
  mk("a0000000-0000-4000-8000-000000000008", "plan child", "COMPLETED", 5, { parent_id: PLAN }),
];
const calls = [];
const ok = (body, status = 200) => ({ status, body });
const respond = (url, method) => {
  const u = new URL(url);
  const p = u.pathname;
  if (p === "/api/v1/tasks" && method === "GET") {
    const parent = u.searchParams.get("parent_id");
    return ok(tasks.filter((t) => !parent || t.parent_id === parent));
  }
  if (p === "/api/v1/tasks" && method === "POST") {
    const t = mk("b0000000-0000-4000-8000-000000000099", "created again", "QUEUED", 0);
    tasks.unshift(t);
    return ok(t, 201);
  }
  if (p === "/api/v1/orchestrator/requests" && method === "POST") {
    calls.push("RUN_AGAIN plan");
    return ok({ orchestration: { id: "o" }, plan: { id: "p" } }, 201);
  }
  const m = p.match(/^\/api\/v1\/tasks\/([^/]+)(\/\w+)?$/);
  if (m) {
    const t = tasks.find((x) => x.id === m[1]);
    if (!t) return ok({ success: false, error: { code: "NOT_FOUND", message: "not found" } }, 404);
    if (!m[2] && method === "GET") return ok(t);
    if (!m[2] && method === "DELETE") {
      tasks = tasks.filter((x) => x.id !== t.id && x.parent_id !== t.id);
      calls.push(`DELETE ${t.title}`);
      return { status: 204, body: undefined };
    }
    if (m[2] === "/resume") {
      t.status = "QUEUED";
      t.failure_reason = undefined;
      calls.push(`RESUME ${t.title}`);
      return ok(t);
    }
    if (m[2] === "/events" || m[2] === "/attempts") return ok([]);
  }
  if (p === "/api/v1/artifacts" || p === "/api/v1/approvals" || p === "/api/v1/managers") return ok([]);
  if (p === "/api/v1/runner/status") return ok({ state: "stopped", scale_to_zero_enabled: true, timestamp: ago(0) });
  if (p === "/api/v1/system/status") {
    const c = (s) => ({ state: s, evidence: "mock", observed_at: ago(0) });
    return ok({ generated_at: ago(0), components: { api: c("ok"), database: c("ok"), worker: c("unknown"), provider: c("unknown"), scheduler: c("unknown"), storage: c("ok"), runner: c("degraded") } });
  }
  return ok({ success: false, error: { code: "NOT_FOUND", message: "no route" } }, 404);
};

const results = [];
const check = (pass, msg) => {
  results.push({ ok: !!pass, msg });
  console.log(`${pass ? "PASS" : "FAIL"}  ${msg}`);
};

const browser = await chromium.launch({ executablePath: "/usr/bin/chromium", args: ["--no-sandbox"] });
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 } });
await ctx.addInitScript(() => {
  localStorage.setItem("griffin_auth_token", "mock.jwt.token");
  localStorage.setItem("griffin_user_profile", JSON.stringify({ id: "u1", email: "operator@example.com", role: "OPERATOR" }));
});
const page = await ctx.newPage();
await page.route(`${API}/**`, async (route) => {
  const req = route.request();
  const cors = { "access-control-allow-origin": BASE, "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
  if (req.method() === "OPTIONS") return route.fulfill({ status: 204, headers: cors });
  const { status, body } = respond(req.url(), req.method());
  await route.fulfill({ status, contentType: "application/json", headers: cors, body: body === undefined ? "" : JSON.stringify(body) });
});
const shot = async (n) => {
  if (SHOTS) await page.screenshot({ path: `${SHOTS}/${n}.png` });
};
const rows = () => page.locator("main a[href^='/tasks/']");
const scrolls = (sel) => page.locator(sel).first().evaluate((el) => ({ v: el.scrollHeight > el.clientHeight + 1, h: el.scrollWidth > el.clientWidth + 1 }));
const rowFor = (title) => page.locator("main div.flex.items-start", { has: page.getByText(title, { exact: true }) }).first();
const menuOf = async (title) => {
  await rowFor(title).getByRole("button", { name: /Actions for/ }).click();
  return page.getByRole("menu");
};
const has = async (loc) => loc.isVisible().catch(() => false);

await page.goto(`${BASE}/tasks`, { waitUntil: "domcontentloaded" });
await rows().first().waitFor();
await shot("tasks-all-390");

// --- layout
const tl = await scrolls("[data-slot=tabs-list]");
check(!tl.v && !tl.h, `filter tabs have no scrollbar (v=${tl.v} h=${tl.h})`);
const pill = page.locator("header.sticky button", { hasText: /asleep/i }).first();
const pb = await pill.boundingBox();
check(pb && pb.height <= 40, `runner chip is a single line (height ${pb?.height})`);
check((await page.locator("header.sticky").innerText()).toLowerCase().includes("wake"), "chip still offers Wake");
const hb = await page.locator("header button[aria-label^='System status']").boundingBox();
check(hb && pb && Math.abs(hb.height - pb.height) < 2, "status button matches the chip height");
check((await rows().count()) === 7, "7 top-level rows in All (subtask hidden)");

// --- done filter
await page.getByRole("tab", { name: "Done" }).click();
check((await rows().count()) === 6, "Done shows 6 finished top-level tasks");
const pick = async (label) => {
  await page.getByLabel("Finished within").click();
  await page.getByRole("option", { name: label }).click();
};
await shot("tasks-done-390");
await pick("Past week");
check((await rows().count()) === 4, "Past week keeps 4 (1d, 2d, 3d, 5d)");
await pick("Today");
check((await rows().count()) === 0, "Today keeps 0");
await shot("tasks-done-empty-390");
await pick("Past month");
check((await rows().count()) === 4, "Past month keeps 4");
await pick("Past 2 months");
check((await rows().count()) === 5, "Past 2 months keeps 5 (adds 40d)");
await pick("Past 3 months");
check((await rows().count()) === 5, "Past 3 months keeps 5 (100d still out)");
await pick("All time");
check((await rows().count()) === 6, "All time shows every finished top-level task");

// --- row menus
let menu = await menuOf("Failed standalone");
check(await has(menu.getByRole("menuitem", { name: "Resume" })), "failed standalone offers Resume");
check(await has(menu.getByRole("menuitem", { name: "Delete" })), "failed standalone offers Delete");
check(!(await has(menu.getByRole("menuitem", { name: "Run again" }))), "failed standalone has no Run again");
await shot("tasks-menu-390");
await page.keyboard.press("Escape");
menu = await menuOf("JJ Orchestration: weekly report");
check(await has(menu.getByRole("menuitem", { name: "Run again" })), "plan offers Run again");
check(!(await has(menu.getByRole("menuitem", { name: "Resume" }))), "plan has no Resume");
await menu.getByRole("menuitem", { name: "Run again" }).click();
await page.waitForTimeout(300);
check(calls.includes("RUN_AGAIN plan"), "Run again on a plan submits a new request");

await page.getByRole("tab", { name: "All" }).click();
check((await rowFor("Running now").getByRole("button", { name: /Actions for/ }).count()) === 0, "running task has no menu");

// --- resume
await page.getByRole("tab", { name: "Done" }).click();
menu = await menuOf("Failed standalone");
await menu.getByRole("menuitem", { name: "Resume" }).click();
await page.waitForTimeout(300);
check(calls.includes("RESUME Failed standalone"), "Resume calls POST /resume");

// --- delete (single, with confirmation)
menu = await menuOf("Cancelled standalone");
await menu.getByRole("menuitem", { name: "Delete" }).click();
await shot("tasks-delete-confirm-390");
await page.getByRole("dialog").getByRole("button", { name: "Delete", exact: true }).click();
await page.waitForTimeout(400);
check(calls.includes("DELETE Cancelled standalone"), "Delete calls DELETE after confirming");
check((await page.getByText("Cancelled standalone", { exact: true }).count()) === 0, "deleted row disappears");

// --- bulk
await page.getByRole("button", { name: "Select", exact: true }).click();
await page.getByRole("button", { name: "Select all" }).click();
await shot("tasks-select-390");
check(await has(page.getByText(/\d+ selected/)), "selection bar shows a count");
// The bar must keep every action on screen on the narrowest phones (it once pushed Delete off at 360px).
for (const w of [320, 360, 390]) {
  await page.setViewportSize({ width: w, height: 740 });
  await page.waitForTimeout(150);
  const boxes = [];
  for (const name of ["Cancel", "Resume", "Delete"]) boxes.push(await page.getByRole("button", { name, exact: true }).first().boundingBox());
  const inside = boxes.every((b) => b && b.x >= 0 && b.x + b.width <= w + 0.5);
  check(inside, `selection actions all fit on screen at ${w}px`);
}
await page.setViewportSize({ width: 390, height: 844 });
await page.getByRole("button", { name: "Delete", exact: true }).first().click();
await page.getByRole("dialog").getByRole("button", { name: "Delete", exact: true }).click();
await page.waitForTimeout(700);
const dels = calls.filter((c) => c.startsWith("DELETE")).length;
check(dels >= 4, `bulk delete issued individual DELETEs (${dels} total)`);
check(!tasks.some((t) => t.id === PLAN || t.parent_id === PLAN), "plan root deleted together with its subtask");

// --- detail page
tasks = [mk("c0000000-0000-4000-8000-000000000001", "Detail failed", "FAILED", 1, { failure_reason: "x" })];
await page.goto(`${BASE}/tasks/c0000000-0000-4000-8000-000000000001`, { waitUntil: "domcontentloaded" });
await page.getByRole("tab", { name: /Overview/ }).waitFor();
const dt = await scrolls("[data-slot=tabs-list]");
check(!dt.v && !dt.h, `detail tabs have no scrollbar (v=${dt.v} h=${dt.h})`);
const tabBox = await page.locator("[data-slot=tabs-list]").boundingBox();
check(tabBox && tabBox.width <= 390, "detail tabs fit the viewport");
check(await has(page.getByRole("button", { name: "Resume" })), "detail shows Resume for a failed task");
check(await has(page.getByRole("button", { name: "Delete" })), "detail shows Delete for a failed task");
await shot("detail-failed-390");
await page.getByRole("button", { name: "Delete" }).click();
await page.getByRole("dialog").getByRole("button", { name: "Delete", exact: true }).click();
await page.waitForURL(/\/tasks$/);
check(calls.includes("DELETE Detail failed"), "detail Delete removes it and returns to the list");

await page.setViewportSize({ width: 1280, height: 800 });
await page.goto(`${BASE}/tasks`, { waitUntil: "domcontentloaded" });
await page.waitForTimeout(500);
await shot("tasks-1280");

await browser.close();
const bad = results.filter((r) => !r.ok);
console.log(`\n${results.length - bad.length}/${results.length} checks passed`);
process.exit(bad.length ? 1 : 0);
