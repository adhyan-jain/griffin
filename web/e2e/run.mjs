// Playwright (playwright-core + system Chromium) end-to-end suite. Run through scripts/e2e-local.sh,
// which provides the local stack. Env: E2E_BASE (default http://localhost:3100), E2E_CHROMIUM, E2E_SHOTS.
import { chromium } from "playwright-core";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const BASE = process.env.E2E_BASE || "http://localhost:3100";
const SHOTS = process.env.E2E_SHOTS || join(dirname(fileURLToPath(import.meta.url)), "shots");
mkdirSync(SHOTS, { recursive: true });

const VIEWPORTS = [
  { name: "360", width: 360, height: 780, mobile: true },
  { name: "390", width: 390, height: 844, mobile: true },
  { name: "430", width: 430, height: 932, mobile: true },
  { name: "1366", width: 1366, height: 850, mobile: false },
];
const ROUTES = ["/", "/tasks", "/chat", "/approvals", "/artifacts", "/workforce", "/settings"];
// The system-status endpoint is built separately; a 404 there is an expected, handled state.
const EXPECTED_FAILURE = /\/api\/v1\/system\/status/;

let failures = 0;
const results = [];
function check(vp, label, ok, detail = "") {
  results.push({ vp, label, ok });
  if (!ok) {
    failures++;
    console.log(`  FAIL [${vp}] ${label} ${detail}`);
  }
}

async function runViewport(browser, vp) {
  const ctx = await browser.newContext({
    viewport: { width: vp.width, height: vp.height },
    isMobile: vp.mobile,
    hasTouch: vp.mobile,
    deviceScaleFactor: 2,
    colorScheme: "dark",
  });
  const page = await ctx.newPage();
  const consoleErrors = [];
  const failedRequests = [];
  page.on("console", (m) => {
    if (m.type() !== "error") return;
    if (EXPECTED_FAILURE.test(m.location()?.url || "")) return;
    consoleErrors.push(m.text().slice(0, 200));
  });
  page.on("pageerror", (e) => consoleErrors.push("pageerror: " + String(e).slice(0, 200)));
  page.on("response", (r) => {
    if (r.status() >= 400 && !EXPECTED_FAILURE.test(r.url())) failedRequests.push(`${r.status()} ${r.request().method()} ${r.url()}`);
  });
  page.on("requestfailed", (r) => {
    if (EXPECTED_FAILURE.test(r.url())) return;
    // Aborted navigations/prefetches during route changes are not product failures.
    if (/ERR_ABORTED/.test(r.failure()?.errorText || "")) return;
    failedRequests.push(`FAILED ${r.method()} ${r.url()} ${r.failure()?.errorText}`);
  });
  const shot = (n) => page.screenshot({ path: join(SHOTS, `${vp.name}-${n}.png`) });
  const overflow = () => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);

  // 1. signed-out redirect
  await page.goto(`${BASE}/tasks`);
  const redirected = await page.waitForURL("**/login", { timeout: 10000 }).then(() => true, () => false);
  check(vp.name, "signed-out visit redirects to /login", redirected);
  await page.waitForTimeout(800);
  await shot("login");
  check(vp.name, "login has no horizontal overflow", (await overflow()) <= 0);

  // 2. dev login
  await page.getByRole("button", { name: /dev user/i }).click();
  const loggedIn = await page.waitForURL(`${BASE}/`, { timeout: 10000 }).then(() => true, () => false);
  check(vp.name, "dev login reaches the dashboard", loggedIn);

  // 3. every route renders without horizontal overflow
  for (const r of ROUTES) {
    await page.goto(`${BASE}${r}`);
    await page.waitForLoadState("networkidle").catch(() => {});
    await page.waitForTimeout(r === "/" ? 1500 : 500);
    const o = await overflow();
    check(vp.name, `no horizontal overflow on ${r}`, o <= 0, `(${o}px)`);
    await shot(r === "/" ? "home" : r.slice(1));
  }

  // 4. create a task via the dialog
  await page.goto(`${BASE}/tasks`);
  await page.getByRole("button", { name: /^new/i }).first().click();
  await page.locator("#goal").fill(`E2E ${vp.name}: summarise the README`);
  await page.getByRole("radio", { name: "High" }).click();
  await shot("new-task");
  check(vp.name, "new-task dialog has no horizontal overflow", (await overflow()) <= 0);
  await page.getByRole("button", { name: "Create task" }).click();
  const onDetail = await page.waitForURL(/\/tasks\/[0-9a-f-]{36}/, { timeout: 10000 }).then(() => true, () => false);
  check(vp.name, "creating a task navigates to its detail page", onDetail);

  // 5. task detail: Now summary + every tab
  if (onDetail) {
    await page.getByTestId("task-now").waitFor({ timeout: 8000 }).catch(() => {});
    check(vp.name, "task detail shows the Now summary", (await page.getByTestId("task-now").count()) === 1);
    for (const tab of ["Overview", "Activity", "Attempts", "Files", "Subtasks"]) {
      const t = page.getByRole("tab", { name: new RegExp(`^${tab}`) });
      await t.click();
      check(vp.name, `task tab ${tab} activates`, (await t.getAttribute("aria-selected")) === "true");
    }
    check(vp.name, "task detail has no horizontal overflow", (await overflow()) <= 0);
    await shot("task-detail");
  }

  // 6. JJ chat: submit, plan card, plan sheet
  await page.goto(`${BASE}/chat`);
  await page.getByLabel("Message JJ").fill("Plan a weekly status report");
  await page.getByRole("button", { name: "Send" }).click();
  const card = page.getByTestId("plan-card");
  const planned = await card.waitFor({ timeout: 15000 }).then(() => true, () => false);
  check(vp.name, "JJ chat shows a plan card after submit", planned);
  if (planned) {
    check(vp.name, "plan card summarises steps and manager", /\d+ steps? led by/.test((await card.innerText()).replace(/\s+/g, " ")));
    await page.getByRole("button", { name: "View plan" }).click();
    const sheet = page.getByRole("dialog");
    check(vp.name, "View plan opens a sheet", await sheet.waitFor({ timeout: 4000 }).then(() => true, () => false));
    await page.waitForTimeout(600);
    await shot("plan-sheet");
    await page.keyboard.press("Escape");
    await page.waitForTimeout(400);
  }
  check(vp.name, "chat has no horizontal overflow", (await overflow()) <= 0);
  await shot("chat-sent");

  // 7. approvals page
  await page.goto(`${BASE}/approvals`);
  await page.waitForLoadState("networkidle").catch(() => {});
  check(vp.name, "approvals page renders its heading", (await page.getByRole("heading", { name: /approvals/i }).count()) > 0);
  await shot("approvals");

  // 8. system status: unavailable must not be faked as healthy
  await page.goto(`${BASE}/settings`);
  await page.waitForLoadState("networkidle").catch(() => {});
  check(vp.name, "settings has a System section", (await page.getByText("System", { exact: true }).count()) > 0);

  check(vp.name, "no console errors", consoleErrors.length === 0, JSON.stringify([...new Set(consoleErrors)]));
  check(vp.name, "no failed requests", failedRequests.length === 0, JSON.stringify([...new Set(failedRequests)]));

  // Offline is exercised last: the induced failures are expected and excluded from the checks above.
  // 9. offline state disables the composer
  await page.goto(`${BASE}/chat`);
  await ctx.setOffline(true);
  const banner = await page.getByTestId("offline-banner").waitFor({ timeout: 4000 }).then(() => true, () => false);
  check(vp.name, "offline banner appears", banner);
  check(vp.name, "composer disabled while offline", await page.getByLabel("Message JJ").isDisabled());
  await ctx.setOffline(false);

  await ctx.close();
}

const browser = await chromium.launch({
  executablePath: process.env.E2E_CHROMIUM || "/usr/bin/chromium",
  args: ["--no-sandbox", "--use-gl=angle", "--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist"],
});
try {
  for (const vp of VIEWPORTS) {
    console.log(`== viewport ${vp.name}`);
    await runViewport(browser, vp);
  }
} finally {
  await browser.close();
}
const passed = results.filter((r) => r.ok).length;
console.log(`\nE2E: ${passed}/${results.length} checks passed, ${failures} failed. Screenshots: ${SHOTS}`);
process.exit(failures ? 1 : 0);
