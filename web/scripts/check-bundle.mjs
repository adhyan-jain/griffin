// Builds the production bundle WITHOUT NEXT_PUBLIC_ENABLE_DEV_LOGIN and fails if any client or
// server artifact contains dev-login UI/API strings or credential-looking material.
// Usage: node scripts/check-bundle.mjs [--skip-build]
import { spawnSync } from "node:child_process";
import { readdirSync, readFileSync, rmSync, statSync } from "node:fs";
import { join } from "node:path";

const DIST = ".next-bundlecheck";
const FORBIDDEN = [
  { name: "Continue as dev user", re: /Continue as dev user/ },
  { name: "auth/login", re: /auth\/login/ },
  { name: "service_role", re: /service_role/ },
  { name: "AWS access key (AKIA...)", re: /AKIA[0-9A-Z]{8,}/ },
];

if (!process.argv.includes("--skip-build")) {
  const env = { ...process.env, NEXT_DIST_DIR: DIST, NODE_ENV: "production" };
  delete env.NEXT_PUBLIC_ENABLE_DEV_LOGIN;
  env.NEXT_PUBLIC_API_URL ||= "https://api.example.invalid";
  const r = spawnSync("npx", ["next", "build"], { env, stdio: "inherit" });
  if (r.status !== 0) {
    console.error("check:bundle: build failed");
    process.exit(1);
  }
}

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    const s = statSync(p);
    if (s.isDirectory()) {
      if (name === "cache" || name === "dev") continue;
      yield* walk(p);
    } else if (/\.(js|mjs|cjs|html|json|css|rsc|txt|map)$/.test(name) && !name.endsWith(".map")) yield p;
  }
}

const hits = [];
let files = 0;
for (const sub of ["static", "server"]) {
  let root;
  try {
    root = join(DIST, sub);
    statSync(root);
  } catch {
    continue;
  }
  for (const file of walk(root)) {
    files++;
    const text = readFileSync(file, "utf8");
    for (const f of FORBIDDEN) if (f.re.test(text)) hits.push(`${f.name}  ->  ${file}`);
  }
}

if (!process.argv.includes("--keep")) rmSync(DIST, { recursive: true, force: true });

if (files === 0) {
  console.error("check:bundle: no build output scanned");
  process.exit(1);
}
if (hits.length) {
  console.error("check:bundle FAILED, forbidden strings in production bundle:\n" + hits.join("\n"));
  process.exit(1);
}
console.log(`check:bundle OK: scanned ${files} files, no forbidden strings.`);
