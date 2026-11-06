// End-to-end check against the REAL stack: the Go server (real worker loop, real Postgres, mock AI and
// storage providers) and the PWA in a real browser. Unlike ui-tasks.mjs there is no mocked API.
//
// Setup (local, throwaway database; ENVIRONMENT=development because production refuses mock providers):
//   docker exec griffin-int-pg psql -U postgres -c "CREATE DATABASE griffin_e2e"
//   export DATABASE_URL=postgres://postgres:postgres@127.0.0.1:55440/griffin_e2e?sslmode=disable
//   go run ./cmd/griffin-migrate -local-auth-shim up
//   ENVIRONMENT=development ENABLE_DEV_LOGIN=true AI_PROVIDER=mock STORAGE_PROVIDER=mock PORT=8099 \
//     CORS_ALLOWED_ORIGINS=http://localhost:3303 SCALE_TO_ZERO_ENABLED=false \
//     SUPABASE_JWT_SECRET=dev-secret-dev-secret-dev-secret-123 WORKER_API_KEY=dev-worker-key-0123456789 \
//     go run ./cmd/griffin-server
//   cd web && NEXT_PUBLIC_ENABLE_DEV_LOGIN=true NEXT_PUBLIC_API_URL=http://localhost:8099 npx next dev -p 3303
// Run (against an EMPTY database; it deletes the tasks it creates):
//   node scripts/e2e-real.mjs [--base http://localhost:3303] [--api http://localhost:8099] [--shots DIR]
import { chromium } from "playwright-core";

const arg = (n, d) => {
  const i = process.argv.indexOf(`--${n}`);
  return i > -1 ? process.argv[i + 1] : d;
};
const BASE = arg("base", "http://localhost:3303");
const API = arg("api", "http://localhost:8099");
const SHOTS = arg("shots", "");

const results = [];
const check = (ok, msg) => {
  results.push(!!ok);
  console.log(`${ok ? "PASS" : "FAIL"}  ${msg}`);
};

const browser = await chromium.launch({ executablePath: "/usr/bin/chromium", args: ["--no-sandbox", "--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist"] });
const page = await (await browser.newContext({ viewport: { width: 1280, height: 860 } })).newPage();
const errs = [];
page.on("pageerror", (e) => errs.push(e.message));
const shot = (n) => (SHOTS ? page.screenshot({ path: `${SHOTS}/${n}.png` }) : null);
const api = (method, path, body) =>
  page.evaluate(
    async ([m, p, b, base]) => {
      const t = localStorage.getItem("griffin_auth_token");
      const r = await fetch(base + p, { method: m, headers: { authorization: `Bearer ${t}`, "content-type": "application/json" }, body: b ? JSON.stringify(b) : undefined });
      return { status: r.status, body: r.status === 204 ? null : await r.json().catch(() => null) };
    },
    [method, path, body, API],
  );

// 1. sign in through the real UI
await page.goto(BASE + "/login", { waitUntil: "domcontentloaded" });
await page.getByRole("button", { name: "Continue as dev user" }).click();
await page.waitForURL(BASE + "/", { timeout: 20000 });
check(true, "dev sign-in lands on home");
await page.waitForTimeout(1500);
check(await page.getByText("Ready when you are").isVisible().catch(() => false), "empty account shows 'Ready when you are'");

// 2. create a task through the dialog and watch the real worker finish it
await page.getByRole("button", { name: /New task/ }).first().click();
await page.getByLabel("What should get done?").fill("Summarise the benefits of unit testing in three bullet points");
await page.getByRole("button", { name: /^(Create|Start|Add)/ }).last().click().catch(() => page.keyboard.press("Enter"));
await page.waitForURL(/\/tasks\/[0-9a-f-]{36}/, { timeout: 20000 });
check(true, "creating a task opens its page");
await page.getByText("Completed").first().waitFor({ timeout: 45000 }).catch(() => {});
check(await page.locator("span").filter({ hasText: /^Completed$/ }).first().isVisible().catch(() => false), "the real worker completed the task");
await page.waitForTimeout(1500);
await shot("task-detail");
const result = page.getByRole("region", { name: "Result" });
check(await result.isVisible().catch(() => false), "a Result reader card is shown");
check(!(await result.evaluate((el) => el.scrollWidth > el.clientWidth + 1).catch(() => true)), "result card has no sideways scroll");

// 2b. the finished answer is kept as a file
await page.getByRole("tab", { name: /^Files/ }).click();
await page.waitForTimeout(800);
check((await page.getByRole("tab", { name: /^Files 1/ }).isVisible().catch(() => false)) && (await page.getByText(/markdown|json/i).first().isVisible().catch(() => false)), "the result is saved as a file on the task's Files tab");
await page.goto(BASE + "/artifacts", { waitUntil: "domcontentloaded" });
await page.waitForTimeout(1200);
check(await page.getByText("Summarise the benefits of unit testing in three bullet points").first().isVisible().catch(() => false), "Files page lists the saved result");

// 3. duplicate plans are refused
check((await api("POST", "/api/v1/orchestrator/requests", { request_text: "Plan a weekly status report" })).status === 201, "plan accepted");
const dup = await api("POST", "/api/v1/orchestrator/requests", { request_text: "Plan a weekly status report" });
check(dup.status === 409, `identical running plan rejected (status ${dup.status})`);
await page.goto(BASE + "/tasks", { waitUntil: "domcontentloaded" });
await page.waitForTimeout(6000);
check((await page.locator("main a[href^='/tasks/']").count()) === 2, "tasks list shows 2 top-level tasks, no duplicates");

// 4. one tap on Run again makes one run; simultaneous identical submits make one plan
await page.getByRole("button", { name: /Actions for JJ Orchestration/ }).first().click();
await page.getByRole("menuitem", { name: "Run again" }).click();
await page.waitForTimeout(6500);
const plans = await api("GET", "/api/v1/tasks?limit=100");
const roots = (plans.body?.data ?? plans.body ?? []).filter((t) => !t.parent_id && t.title.startsWith("JJ Orchestration"));
check(roots.length === 2, `one tap on Run again created one new run (total ${roots.length}, expected 2)`);
const burst = await page.evaluate(async ([base]) => {
  const t = localStorage.getItem("griffin_auth_token");
  const go = () => fetch(base + "/api/v1/orchestrator/requests", { method: "POST", headers: { authorization: `Bearer ${t}`, "content-type": "application/json" }, body: JSON.stringify({ request_text: "Plan the quarterly review" }) }).then((r) => r.status);
  return Promise.all([go(), go(), go()]);
}, [API]);
check(burst.filter((s) => s === 201).length === 1 && burst.filter((s) => s === 409).length === 2, `3 simultaneous identical submits: exactly one accepted (${burst.join(",")})`);
await page.waitForTimeout(5000);

// 5. Done filter, selection and bulk delete
await page.goto(BASE + "/tasks", { waitUntil: "domcontentloaded" });
await page.getByRole("tab", { name: "Done" }).click();
check(await page.getByLabel("Finished within").isVisible().catch(() => false), "Done tab offers the time-window filter");
await page.getByRole("button", { name: "Select" }).click();
await page.getByRole("button", { name: /Select all/ }).click();
await shot("selection");
await page.getByRole("button", { name: "Delete" }).last().click();
await page.getByRole("button", { name: "Delete", exact: true }).last().click();
await page.waitForTimeout(1500);
check((await page.locator("main a[href^='/tasks/']").count()) === 0, "bulk delete removed the finished tasks");

check(errs.length === 0, `no page errors ${errs.slice(0, 2).join(" | ")}`);
console.log(`${results.filter(Boolean).length}/${results.length} checks passed`);
await browser.close();
process.exit(results.every(Boolean) ? 0 : 1);
