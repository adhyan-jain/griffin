# GRIFFIN — Cloud Deployment Preflight Report

> **UPDATE 2026-11-02 — gates G1–G8 have been closed in code.** This report records the state
> *before* the hardening sprint (findings, live AWS evidence, IAM policies, cost, checklists remain
> valid). The fixes, tests and exact results are in `docs/GATE_CLOSURE_REPORT.md`; the commands that
> changed are in `docs/DEPLOYMENT_RUNBOOK.md`. Sections 3 (gates) and 4 (agy auth) describe the
> original defects; items still NOT VERIFIED/BLOCKED after the sprint are listed at the end of the
> closure report. Deployment has not been performed and awaits explicit approval.


- Date: 2026-11-02
- Repo: `/home/adhyan/Desktop/griffin`, `master` @ `55c934a` (clean tree, equals `origin/master`)
- AWS account `220115213279`, region `ap-south-1`, caller `arn:aws:iam::220115213279:user/griffin-bootstrap`
- Method: read-only. No AWS resource was created or modified, nothing deployed, no secret value read or printed. (`.env` was inspected for key *names* only.)
- Scratch artifacts (web build, arm64 cross-compile, agy installer copy) are in the session scratchpad, not the repo.

Status vocabulary: **IMPLEMENTED** (code/script exists) · **VERIFIED** (checked live or by execution in this preflight) · **MISSING** · **BLOCKED** (cannot proceed without a decision/permission/fix) · **NOT VERIFIED** (could not check).

---

## 0. Verdict: NO-GO for production deployment as the repo stands

AWS-side preparation is small and mostly safe. The **application is not production-safe**. Eight code/config gates (section 3) must be closed first; two of them mean the system would be *openly exploitable* or *would never scale to zero* if deployed today.

| # | Gate | Severity |
|---|------|----------|
| G1 | `POST /api/v1/auth/login` is unauthenticated, mints a 7-day JWT for **any** email (incl. existing real users) | CRITICAL |
| G2 | CORS reflects any `Origin` with `Allow-Credentials: true` | HIGH |
| G3 | Dev-default `SUPABASE_JWT_SECRET` / `WORKER_API_KEY` are silently used if env is unset | HIGH |
| G4 | `EC2InstanceManager` is a **stub** — wake-up never calls AWS | BLOCKER (scale-to-zero broken) |
| G5 | Idle shutdown runs `sudo shutdown -h now`, but the `griffin` user has no sudo → instance never stops | BLOCKER (cost) |
| G6 | Migration 000001 replaces `auth.uid()`/creates `auth.users` shim; server re-runs all migrations on every start | BLOCKER on Supabase |
| G7 | `CreateArtifact` accepts client-supplied `bucket`/`object_key` → cross-tenant read/delete | HIGH |
| G8 | No Supabase sign-in in the PWA; no systemd unit; no Lambda packaging; no DB pooler-mode handling | BLOCKER |

---

## 1. Existing resources & verified status

| Resource | Status | Evidence |
|---|---|---|
| AWS CLI profile `griffin-bootstrap` → account 220115213279 | VERIFIED | `sts get-caller-identity` |
| S3 `griffin-artifacts-220115213279-ap-south-1`, region ap-south-1 | VERIFIED | `head-bucket` |
| — SSE AES256 (SSE-C blocked) | VERIFIED | `get-bucket-encryption` |
| — Block Public Access, all 4 flags on | VERIFIED | `get-public-access-block` |
| — Ownership `BucketOwnerEnforced` | VERIFIED | `get-bucket-ownership-controls` |
| — Versioning **Enabled** (runbook doesn't mention it) | VERIFIED | `get-bucket-versioning` |
| — Lifecycle config | MISSING | `NoSuchLifecycleConfiguration` → old versions/delete markers accrue forever |
| IAM role `GriffinEC2WorkerRole` | MISSING | `NoSuchEntity` |
| Instance profile `GriffinEC2WorkerProfile` | MISSING | `NoSuchEntity` |
| IAM role `GriffinSchedulerRole` (runbook §5) | MISSING | `NoSuchEntity` |
| EC2 runner instance, SG `griffin-runner-sg`, default VPC, AMI `ami-0d8b4e4e9a59cf861` | NOT VERIFIED | `ec2:Describe*` AccessDenied (see §5) |
| EventBridge schedules, Lambda functions, SSM parameters | NOT VERIFIED | AccessDenied |
| Supabase project | NOT VERIFIED | no CLI, no credentials, no project ref in repo |
| Domain / DNS / ACM certificate | BLOCKED | repo uses placeholders `api.griffin.workforce` / `api.griffin.internal`; no real domain supplied |
| Local tools: aws, psql, jq, zip, node 24, npm, vercel, docker, go 1.27 | VERIFIED | |
| Local tools: `session-manager-plugin`, `supabase` CLI | MISSING | needed for `aws ssm start-session` / optional |

IAM note: `iam:GetRole`/`GetInstanceProfile` return `NoSuchEntity` (not AccessDenied), so some IAM read exists; `iam:ListAttachedUserPolicies` is denied, so the user's current policy set cannot be enumerated. Only effective permissions observed: S3 bucket-config reads, IAM Get*.

## 2. Missing resources (full list to create)

1. IAM: `GriffinEC2WorkerRole` + profile (script creates), `griffin-api-lambda-role`, `griffin-events-invoke-role`.
2. EC2 `t4g.medium` runner + `griffin-runner-sg` (script creates).
3. SSM SecureString parameters under `/griffin/prod/*`.
4. Lambda `griffin-api` (arm64, `provided.al2023`) + Lambda Web Adapter layer; API Gateway HTTP API + custom domain + ACM cert (ap-south-1).
5. EventBridge connection + API destination + rule `rate(1 minute)` for `/scheduler/tick`.
6. S3 lifecycle rule (noncurrent version expiry).
7. Supabase project (DB, Auth, JWT settings); Vercel project for the PWA; DNS records.
8. Budget alarm (cost guardrail).

---

## 3. Application gates (must be closed before deploy)

Not implemented here (out of scope: "no feature work"). Each is small.

**G1 — Dev login in production (CRITICAL).** `internal/api/server.go:74` registers `POST /api/v1/auth/login` unconditionally; `internal/api/handlers/auth.go:48-105` takes `{email}`, looks up/INSERTs into `auth.users` and `profiles`, signs HS256 with the *real* `SUPABASE_JWT_SECRET`, `exp` 7 days, no password. With a real project this is full account takeover and bypasses Supabase Auth. The PWA ships "Use Quick Dev Login (admin@griffin.internal)" (`web/src/app/login/page.tsx`) and `api.login()` (`web/src/lib/api.ts`). **Fix:** register the route only when `ENVIRONMENT != production` (or delete it); remove the button from the production bundle.

**G2 — CORS.** `internal/api/middleware/cors.go:10-22` echoes the request Origin (`*` if absent) with `Allow-Credentials: true`. **Fix:** allowlist from a `CORS_ALLOWED_ORIGINS` env var; no wildcard.

**G3 — Insecure defaults.** `internal/config/config.go:55-56` default `SUPABASE_JWT_SECRET`/`WORKER_API_KEY` to public dev strings, and `DATABASE_URL` to localhost. **Fix:** in `Load()`, when `ENVIRONMENT=production`, fail if either equals the default or is <32 chars. Also use `subtle.ConstantTimeCompare` for the worker key (`middleware/auth.go:97`).

**G4 — Wake-up is a stub.** `internal/runner/instance.go:119-134`: `StartRunner`/`StopRunner` return nil, `GetRunnerState` returns `unknown`. `go.mod` has no `service/ec2` module. Result: tasks queue, the runner never starts. **Fix:** implement with `ec2:StartInstances/StopInstances/DescribeInstances` (add `github.com/aws/aws-sdk-go-v2/service/ec2`).

**G5 — Instance never stops.** `internal/runner/shutdown.go:72` runs `sudo shutdown -h now`; `infra/user-data.sh` creates `griffin` with no sudoers entry (and `docs/experiments/EC2_AGY_POC.md` states "without sudo"). Fixed in bootstrap (sudoers drop-in, §13) with no code change.

**G6 — Migrations vs Supabase.**
- `migrations/000001_init_schema.up.sql:8-21` does `CREATE OR REPLACE FUNCTION auth.uid()` with a `request.jwt.claim.sub` shim. On Supabase this either fails (not owner) or overwrites the platform function. NOT VERIFIED which; both are unacceptable.
- `cmd/griffin-server/main.go:71` runs *every* `migrations/*.up.sql` on every process start (no version table), from the relative dir `migrations`. Concurrent Lambda cold starts + worker would race on DDL.
- `Makefile` `migrate` and runbook §3 target `localhost:54322`/placeholder hosts.
- **No-code workaround (VERIFIED by diff):** strip the shim with the sed command in §10, apply manually, and ship an *empty* `migrations/` dir (only `.keep`) with each binary so `RunMigrations` is a no-op.
- RLS is `FORCE`d, but the backend connects as `postgres` (bypasses RLS) and never sets `request.jwt.claim.*`; RLS therefore protects only PostgREST/anon access. Tenant isolation in the API relies on `user_id` predicates in the repositories (see §8). Do **not** switch `DATABASE_URL` to a non-BYPASSRLS role without code changes (every query would return 0 rows).

**G7 — Artifact tenancy hole.** `internal/api/handlers/artifacts.go:98-143` stores client-supplied `bucket`, `object_key`, `storage_uri` verbatim. `Download/Presign/Delete` then use the stored `bucket`/`object_key` (`internal/storage/service.go:246-306`). Any authenticated user can register an artifact pointing at another tenant's key (`tasks/<uuid>/outputs/<file>`) or any bucket the API role can read, then download/presign/delete it. Upload (`:146-242`) also accepts any `task_id` without ownership check. **Fix:** in `CreateArtifact` ignore client bucket/key (force service bucket + server-generated key, or reject), and verify `task_id` belongs to `userID`. Interim: do not expose `POST /artifacts` publicly.

**G8 — Missing deployment plumbing.**
- PWA has no Supabase sign-in (no `@supabase/supabase-js`, no sign-in UI; only dev login or pasting a JWT).
- No systemd unit for `griffin-worker`; no env/secrets loader on EC2.
- No Lambda entrypoint/packaging, no Dockerfile, no IaC. (`docs/SERVERLESS_API_DESIGN.md` selects Lambda but nothing implements it.)
- pgx uses default settings (`internal/database/postgres.go:18-29`); Supabase transaction pooler (6543) needs `default_query_exec_mode=simple_protocol` in the URL.
- JWT middleware accepts **HS256 only** (`auth.go:51`). Supabase projects using asymmetric signing keys issue ES256/RS256 tokens → every request 401. NOT VERIFIED which your project uses; check *Settings → JWT Keys*, use the legacy HS256 secret or add JWKS verification. No `aud`/`iss` check.
- `GET /api/v1/runner/status` is unauthenticated (`server.go:145`); low risk, leaks runner state.
- Artifact upload buffers the whole body in memory (`storage/service.go:143`) and server `WriteTimeout` is 30 s; Lambda request cap is 6 MB, API Gateway 10 MB.
- `.env` contains `AWS_S3_BUCKET`, but the code reads `S3_BUCKET` (default `griffin-artifacts`, the *wrong* bucket); Go does not load `.env` at all. `.env.example` is missing most variables (§9).
- Runbook says idle shutdown 10 min; code default is 300 s (`IDLE_SHUTDOWN_SECONDS`).
- Docs/product say Next.js 15; `web/package.json` pins `next 16.4.3`.
- `web` is not built or linted in CI; PWA has a manifest but no service worker and only a `.ico` icon (not installable per Chrome criteria; non-blocking).

---

## 4. Antigravity (`agy`) unattended authentication

### Documented (official page, fetched `https://antigravity.google/docs/install/`)
- Interactive remote login: run `agy` over SSH/SSM → prints auth URL → sign in on another device → paste the code in the terminal.
- Credentials are stored as "token profiles", using the OS keyring (Linux Secret Service/dbus named). With a valid profile the CLI "authenticates silently".
- Truly unattended modes documented: **(a) `GEMINI_API_KEY`** + `~/.gemini/antigravity-cli/settings.json` `{"modelProvider":"gemini"}` (explicitly "suits headless and CI"); **(b) ADC**: `gcloud auth application-default login` + `AGY_ADC_AUTH=true`.
- **Not documented:** token file path, DBus/keyring setup for headless Linux, behavior with no keyring, refresh-token lifetime/re-auth cadence, any non-interactive OAuth login.

### Verified locally (agy 1.2.0, `agy --help`, subcommand listing)
- Real flags: `-p/--print`, `--output-format text|json|stream-json`, `--conversation`, `--model`, `--json-schema`, `--print-timeout`, `--disable-slash-commands`. `internal/provider/antigravity/executor.go:196-209` uses real flags (OK).
- There is **no `auth` subcommand**, no `--no-browser`, no `--format`, no `--stream`. Subcommands: agent(s), changelog, help, install, mcp, mic-serve, models, plugin(s), remote-control, update.
- The local state dir `~/.gemini/antigravity-cli/` contains no `auth.json` (names listed only; contents not read).
- Installer: maps `aarch64 → linux_arm64`; manifest returned HTTP 200, version 1.4.12 for both arm64 and amd64 (VERIFIED). Local version is 1.2.0 → version drift.

### Consequences for `docs/ANTIGRAVITY_CLOUD_RUNNER.md`
| Claim | Status |
|---|---|
| `agy auth login --no-browser` | **Contradicted** by CLI help; remove |
| Seed `/home/griffin/.gemini/antigravity-cli/auth.json` from an SSM-stored "refresh token" (Option A) | **Unsupported / invented**; no such file or token export exists in docs or CLI; do not build on it |
| `agy --format json --stream` | **Wrong flags**; real: `--output-format json`/`stream-json` |
| Tokens "persist across stop/start because EBS DeleteOnTermination:false" | Plausible for on-disk state; **unproven** for keyring-held state (see R1) |
| Refresh "automatic, indefinite while subscribed" (EC2_AUTH_PERSISTENCE.md) | **Assumption**; the doc itself marks Cloud VM tests "NOT TESTED" |
| `loginctl enable-linger griffin`, `DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1001/bus` | Reasonable standard Linux practice; NOT VERIFIED for agy |

### Supported procedure to use (and risks)
1. **Primary (matches your "subscription" goal, one-time interactive):** SSM into the instance as `griffin`, run `agy` once, complete the URL/code flow (§14). Then test a reboot and a stop/start.
2. **Fallback that is fully documented and unattended:** `GEMINI_API_KEY` mode (per-token billing — contradicts the "no per-token billing" objective) or ADC.

Unresolved risks (all NOT VERIFIED until tested on the instance):
- **R1 Keyring across reboot.** Secret Service via gnome-keyring in a lingering `systemd --user` session normally starts *locked* with no login password; whether agy falls back to a file store or fails after reboot is undocumented. Test: authenticate, `sudo reboot`, run `agy -p ping --output-format json` from a context with no SSM session open.
- **R2 Refresh lifetime / Google re-verification** from an AWS IP is unknown. Mitigation: canary every 6 h (the executor marks the provider unhealthy on `ErrAuthenticationRequired`, `executor.go:326`; tasks should park, not fall back silently).
- **R3 Version drift** (local 1.2.0 vs installer 1.4.12): re-check flags with `agy --help` on the instance; pin the version.
- **R4 Terms of use** of running a personal-subscription CLI on a server: confirm with provider terms.
- **R5 Concurrency/memory:** `MAX_CONCURRENT_ANTIGRAVITY` capped at 2 in code; the 235 MB/process figure is from docs, unmeasured on arm64.

Status: interactive auth procedure DOCUMENTED; unattended persistence NOT VERIFIED; runner doc §2.2 BLOCKED (unsupported).

---

## 5. `infra/provision-ec2.sh` review and IAM

### 5.1 Script review

| Aspect | Finding | Status |
|---|---|---|
| Instance | `t4g.medium` (Graviton2, 2 vCPU/4 GiB), default VPC/subnet, public IPv4 auto-assigned (required: no NAT, outbound only) | IMPLEMENTED |
| AMI | Resolves latest Canonical Ubuntu 24.04 **arm64** (`owner 099720109477`) via `ec2:DescribeImages`; **silently falls back** to hard-coded `ami-0d8b4e4e9a59cf861` when describe is denied (it is, today). That ID could not be verified. Prefer the public SSM parameter `/aws/service/canonical/ubuntu/server/24.04/stable/current/arm64/hvm/ebs-gp3/ami-id` | NOT VERIFIED / risk |
| arm64 + `agy` | Installer/manifest for `linux_arm64` exist (HTTP 200, 1.4.12). Go server cross-compiles for linux/arm64 (`CGO_ENABLED=0`), `go vet ./...` clean | VERIFIED (availability) |
| IAM | Creates role + `AmazonSSMManagedInstanceCore` + inline S3 policy **only if role is new** (a partial failure leaves a role without policies; rerun skips them). S3 resource `griffin-artifacts-*` is a wildcard; `s3:HeadObject` is not a real IAM action; multipart needs `s3:AbortMultipartUpload` | IMPLEMENTED, needs tightening |
| Security group | Created in default VPC with **zero inbound** (default egress all). `create-security-group … \|\| true` can leave `SG_ID` empty/error text → instance would launch in the default SG | IMPLEMENTED, fragile |
| EBS | 30 GiB gp3 root, `Encrypted:true` (default `aws/ebs` key), `DeleteOnTermination:false` → persists across stop/start but **orphans a billed volume if terminated** | IMPLEMENTED |
| IMDS | Not set → IMDSv1 allowed. Add `HttpTokens=required` | MISSING |
| Shutdown behavior | Not explicit (default `stop` for EBS-backed). Make explicit | MISSING |
| User-data | Installs packages, SSM agent, user `griffin` (uid 1001), `enable-linger`, 2 GiB swap, `/var/griffin/workspaces`, `agy` via installer. **Does not**: install `dbus-user-session` explicitly, configure/unlock a keyring, add sudoers, install the Griffin binary, create a systemd unit, install AWS CLI, or load env/secrets. `agy --version` failure is swallowed (`\|\| true`). Runs only on first boot | IMPLEMENTED, incomplete |
| SSM | `AmazonSSMManagedInstanceCore` attached; outbound 443 OK. `session-manager-plugin` not installed locally | IMPLEMENTED / local MISSING |
| Start/stop permissions | Instance self-stops via OS. API needs `ec2:StartInstances/StopInstances/DescribeInstances` on the one instance ARN (granted nowhere yet; §5.4) | MISSING |
| Idempotency | **Not idempotent.** `run-instances` always launches a new instance (no tag lookup, no `--client-token`). `file://infra/user-data.sh` is relative → run from repo root. Use the guard below | MISSING |

Pre-launch idempotency guard:
```bash
aws ec2 describe-instances --profile griffin-bootstrap --region ap-south-1 \
  --filters Name=tag:Name,Values=griffin-cloud-runner Name=instance-state-name,Values=pending,running,stopping,stopped \
  --query 'Reservations[].Instances[].InstanceId' --output text   # must be empty
```

### 5.2 What is missing from `griffin-bootstrap` (verified denials)
`ec2:DescribeInstances`, `ec2:DescribeImages`, `ec2:DescribeVpcs`, `ec2:DescribeSecurityGroups`, `scheduler:ListSchedules`, `lambda:ListFunctions`, `ssm:DescribeParameters`, `pricing:GetProducts`, `iam:ListAttachedUserPolicies`. Not tested (would mutate): RunInstances, CreateSecurityGroup, CreateRole, etc. — assume missing. The runbook's §2.2 policy is **insufficient** (no `iam:CreateRole/AttachRolePolicy/PutRolePolicy/CreateInstanceProfile/AddRoleToInstanceProfile/GetRole`, no SSM session rights) and **over-broad** (`TerminateInstances` on `*`, `Resource: *`).

Attach with an admin identity (not `griffin-bootstrap`; it cannot self-grant). Replace `ADMIN_PROFILE`.

### 5.3 Policy A — exactly what `provision-ec2.sh` + SSM access needs

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Sid": "Ec2Describe", "Effect": "Allow",
      "Action": ["ec2:DescribeImages","ec2:DescribeVpcs","ec2:DescribeSubnets","ec2:DescribeSecurityGroups",
                 "ec2:DescribeInstances","ec2:DescribeInstanceStatus","ec2:DescribeVolumes","ec2:DescribeTags"],
      "Resource": "*",
      "Condition": { "StringEquals": { "aws:RequestedRegion": "ap-south-1" } } },

    { "Sid": "RunInstancesNonInstanceResources", "Effect": "Allow", "Action": "ec2:RunInstances",
      "Resource": [
        "arn:aws:ec2:ap-south-1::image/*",
        "arn:aws:ec2:ap-south-1:220115213279:subnet/*",
        "arn:aws:ec2:ap-south-1:220115213279:network-interface/*",
        "arn:aws:ec2:ap-south-1:220115213279:security-group/*" ] },

    { "Sid": "RunInstancesInstanceType", "Effect": "Allow", "Action": "ec2:RunInstances",
      "Resource": "arn:aws:ec2:ap-south-1:220115213279:instance/*",
      "Condition": { "StringEquals": { "ec2:InstanceType": "t4g.medium" } } },

    { "Sid": "RunInstancesEncryptedVolumesOnly", "Effect": "Allow", "Action": "ec2:RunInstances",
      "Resource": "arn:aws:ec2:ap-south-1:220115213279:volume/*",
      "Condition": { "Bool": { "ec2:Encrypted": "true" } } },

    { "Sid": "TagOnCreate", "Effect": "Allow", "Action": "ec2:CreateTags",
      "Resource": ["arn:aws:ec2:ap-south-1:220115213279:instance/*",
                   "arn:aws:ec2:ap-south-1:220115213279:volume/*"],
      "Condition": { "StringEquals": { "ec2:CreateAction": "RunInstances" } } },

    { "Sid": "CreateRunnerSecurityGroup", "Effect": "Allow", "Action": "ec2:CreateSecurityGroup",
      "Resource": ["arn:aws:ec2:ap-south-1:220115213279:vpc/*",
                   "arn:aws:ec2:ap-south-1:220115213279:security-group/*"] },

    { "Sid": "WorkerRoleBootstrap", "Effect": "Allow",
      "Action": ["iam:GetRole","iam:CreateRole","iam:PutRolePolicy","iam:GetRolePolicy",
                 "iam:GetInstanceProfile","iam:CreateInstanceProfile","iam:AddRoleToInstanceProfile"],
      "Resource": ["arn:aws:iam::220115213279:role/GriffinEC2WorkerRole",
                   "arn:aws:iam::220115213279:instance-profile/GriffinEC2WorkerProfile"] },

    { "Sid": "AttachOnlySsmCorePolicy", "Effect": "Allow", "Action": "iam:AttachRolePolicy",
      "Resource": "arn:aws:iam::220115213279:role/GriffinEC2WorkerRole",
      "Condition": { "ArnEquals": { "iam:PolicyARN": "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore" } } },

    { "Sid": "PassWorkerRoleToEc2Only", "Effect": "Allow", "Action": "iam:PassRole",
      "Resource": "arn:aws:iam::220115213279:role/GriffinEC2WorkerRole",
      "Condition": { "StringEquals": { "iam:PassedToService": "ec2.amazonaws.com" } } },

    { "Sid": "ResolveUbuntuAmiViaSsmPublicParam", "Effect": "Allow", "Action": "ssm:GetParameter",
      "Resource": "arn:aws:ssm:ap-south-1::parameter/aws/service/canonical/ubuntu/server/24.04/stable/current/arm64/hvm/ebs-gp3/ami-id" },

    { "Sid": "SessionManagerToGriffinRunner", "Effect": "Allow", "Action": "ssm:StartSession",
      "Resource": ["arn:aws:ec2:ap-south-1:220115213279:instance/*",
                   "arn:aws:ssm:ap-south-1:*:document/SSM-SessionManagerRunShell"],
      "Condition": { "StringEquals": { "ssm:resourceTag/Project": "Griffin" } } },

    { "Sid": "SessionManagerHousekeeping", "Effect": "Allow",
      "Action": ["ssm:DescribeInstanceInformation","ssm:DescribeSessions","ssm:GetConnectionStatus"],
      "Resource": "*" },

    { "Sid": "SessionManagerOwnSessions", "Effect": "Allow",
      "Action": ["ssm:TerminateSession","ssm:ResumeSession"],
      "Resource": "arn:aws:ssm:*:*:session/${aws:userid}-*" }
  ]
}
```
Notes: `Describe*` cannot be resource-scoped. The tag condition on `StartSession` works because the script tags `Project=Griffin`; the document-resource condition may need adjusting if the tag condition is rejected on the document ARN (NOT VERIFIED — if `start-session` is denied, split the document statement without the condition).

Apply:
```bash
aws iam put-user-policy --user-name griffin-bootstrap --policy-name GriffinBootstrapEc2Provision \
  --policy-document file://policy-a.json --profile ADMIN_PROFILE
```

### 5.4 Policy B — only for the API ingress / scheduler / secrets steps (Phases 4, 7)
Attach later, separately.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Sid": "LambdaApi", "Effect": "Allow",
      "Action": ["lambda:CreateFunction","lambda:UpdateFunctionCode","lambda:UpdateFunctionConfiguration",
                 "lambda:GetFunction","lambda:GetFunctionConfiguration","lambda:PublishVersion",
                 "lambda:CreateAlias","lambda:UpdateAlias","lambda:AddPermission","lambda:TagResource",
                 "lambda:PutFunctionConcurrency"],
      "Resource": ["arn:aws:lambda:ap-south-1:220115213279:function:griffin-api",
                   "arn:aws:lambda:ap-south-1:220115213279:function:griffin-api:*"] },
    { "Sid": "ReadAdapterLayer", "Effect": "Allow",
      "Action": ["lambda:GetLayerVersion","lambda:ListLayerVersions"],
      "Resource": "arn:aws:lambda:ap-south-1:753240598075:layer:LambdaAdapterLayerArm64*" },
    { "Sid": "ApiRoles", "Effect": "Allow",
      "Action": ["iam:CreateRole","iam:GetRole","iam:PutRolePolicy","iam:AttachRolePolicy","iam:TagRole"],
      "Resource": ["arn:aws:iam::220115213279:role/griffin-api-lambda-role",
                   "arn:aws:iam::220115213279:role/griffin-events-invoke-role"] },
    { "Sid": "PassApiRoles", "Effect": "Allow", "Action": "iam:PassRole",
      "Resource": ["arn:aws:iam::220115213279:role/griffin-api-lambda-role",
                   "arn:aws:iam::220115213279:role/griffin-events-invoke-role"],
      "Condition": { "StringEquals": { "iam:PassedToService": ["lambda.amazonaws.com","events.amazonaws.com"] } } },
    { "Sid": "WorkerRoleExtraPolicy", "Effect": "Allow", "Action": "iam:PutRolePolicy",
      "Resource": "arn:aws:iam::220115213279:role/GriffinEC2WorkerRole" },
    { "Sid": "Logs", "Effect": "Allow",
      "Action": ["logs:CreateLogGroup","logs:PutRetentionPolicy","logs:DescribeLogGroups"],
      "Resource": "arn:aws:logs:ap-south-1:220115213279:log-group:/aws/lambda/griffin-api*" },
    { "Sid": "Secrets", "Effect": "Allow",
      "Action": ["ssm:PutParameter","ssm:GetParameter","ssm:GetParametersByPath","ssm:AddTagsToResource"],
      "Resource": "arn:aws:ssm:ap-south-1:220115213279:parameter/griffin/prod/*" },
    { "Sid": "SsmList", "Effect": "Allow", "Action": "ssm:DescribeParameters", "Resource": "*" },
    { "Sid": "EventBridgeTick", "Effect": "Allow",
      "Action": ["events:PutRule","events:DescribeRule","events:EnableRule","events:DisableRule","events:PutTargets",
                 "events:RemoveTargets","events:DeleteRule","events:CreateConnection","events:DescribeConnection",
                 "events:UpdateConnection","events:CreateApiDestination","events:DescribeApiDestination",
                 "events:UpdateApiDestination"],
      "Resource": ["arn:aws:events:ap-south-1:220115213279:rule/griffin-*",
                   "arn:aws:events:ap-south-1:220115213279:connection/griffin-*",
                   "arn:aws:events:ap-south-1:220115213279:api-destination/griffin-*"] },
    { "Sid": "EventBridgeConnectionSecret", "Effect": "Allow",
      "Action": ["secretsmanager:CreateSecret","secretsmanager:DescribeSecret","secretsmanager:GetSecretValue",
                 "secretsmanager:PutSecretValue","secretsmanager:UpdateSecret"],
      "Resource": "arn:aws:secretsmanager:ap-south-1:220115213279:secret:events!connection/griffin-*" },
    { "Sid": "HttpApiAndDomain", "Effect": "Allow",
      "Action": ["apigateway:GET","apigateway:POST","apigateway:PUT","apigateway:PATCH","apigateway:DELETE"],
      "Resource": ["arn:aws:apigateway:ap-south-1::/apis","arn:aws:apigateway:ap-south-1::/apis/*",
                   "arn:aws:apigateway:ap-south-1::/domainnames","arn:aws:apigateway:ap-south-1::/domainnames/*",
                   "arn:aws:apigateway:ap-south-1::/tags/*"] },
    { "Sid": "RegionalCertificate", "Effect": "Allow",
      "Action": ["acm:RequestCertificate","acm:DescribeCertificate","acm:ListCertificates","acm:AddTagsToCertificate"],
      "Resource": "*", "Condition": { "StringEquals": { "aws:RequestedRegion": "ap-south-1" } } },
    { "Sid": "S3Lifecycle", "Effect": "Allow",
      "Action": ["s3:GetLifecycleConfiguration","s3:PutLifecycleConfiguration"],
      "Resource": "arn:aws:s3:::griffin-artifacts-220115213279-ap-south-1" },
    { "Sid": "BudgetAlarm", "Effect": "Allow", "Action": ["budgets:ViewBudget","budgets:ModifyBudget"],
      "Resource": "arn:aws:budgets::220115213279:budget/griffin-*" }
  ]
}
```
If DNS is in Route 53 add `route53:ChangeResourceRecordSets`/`ListHostedZones` for that zone (DNS provider unknown → BLOCKED). `apigateway:*` cannot be scoped tighter than path families. The Lambda Web Adapter layer account/ARN (`753240598075`) is from memory → NOT VERIFIED; confirm with the command in §15. The Secrets Manager statement for the EventBridge connection secret follows AWS docs for connection creators → NOT VERIFIED until first run. `budgets:CreateBudget` is not included; create the budget in the console or add it.

### 5.5 Roles the deployment creates (workloads)

**Worker instance role (`GriffinEC2WorkerRole`)** — replace the script's inline policy after launch (§13):
```json
{ "Version":"2012-10-17","Statement":[
  {"Sid":"ArtifactsRW","Effect":"Allow",
   "Action":["s3:PutObject","s3:GetObject","s3:DeleteObject","s3:AbortMultipartUpload"],
   "Resource":"arn:aws:s3:::griffin-artifacts-220115213279-ap-south-1/*"},
  {"Sid":"ArtifactsList","Effect":"Allow","Action":"s3:ListBucket",
   "Resource":"arn:aws:s3:::griffin-artifacts-220115213279-ap-south-1"},
  {"Sid":"ReadOwnSecrets","Effect":"Allow","Action":["ssm:GetParameter","ssm:GetParametersByPath"],
   "Resource":"arn:aws:ssm:ap-south-1:220115213279:parameter/griffin/prod/worker*"}
]}
```

**API Lambda role (`griffin-api-lambda-role`)**: AWS managed `AWSLambdaBasicExecutionRole` plus
```json
{ "Version":"2012-10-17","Statement":[
  {"Sid":"WakeRunnerOnly","Effect":"Allow",
   "Action":["ec2:StartInstances","ec2:StopInstances"],
   "Resource":"arn:aws:ec2:ap-south-1:220115213279:instance/<INSTANCE_ID>"},
  {"Sid":"RunnerState","Effect":"Allow","Action":"ec2:DescribeInstances","Resource":"*"},
  {"Sid":"ArtifactsRW","Effect":"Allow",
   "Action":["s3:PutObject","s3:GetObject","s3:DeleteObject","s3:AbortMultipartUpload"],
   "Resource":"arn:aws:s3:::griffin-artifacts-220115213279-ap-south-1/*"},
  {"Sid":"ArtifactsList","Effect":"Allow","Action":"s3:ListBucket",
   "Resource":"arn:aws:s3:::griffin-artifacts-220115213279-ap-south-1"}
]}
```
(`<INSTANCE_ID>` is known only after provisioning.) `StopInstances` only serves `POST /runner/sleep`; drop it to remove that capability.

**`griffin-events-invoke-role`**: trust `events.amazonaws.com`; permission `events:InvokeApiDestination` on the API destination ARN.

---

## 6. Production auth review (the six confirmations)

| # | Question | Answer |
|---|---|---|
| 1 | Quick Dev Login impossible in prod? | **NO.** Backend route exists in all envs (G1); button is in the production bundle. |
| 2 | Supabase auth enforced? | **Partially.** All `/api/v1` user routes except login/health/runner-status pass `AuthenticateUser` (HS256 + exp + uuid `sub`). But login (G1) mints valid tokens; HS256-only (G8); no `aud`/`iss`; PWA cannot sign in via Supabase. |
| 3 | CORS restricted? | **NO** (G2). |
| 4 | Service-role key / AWS creds never reach browser? | **YES (VERIFIED by grep):** `web/src` has no service-role, `AWS_`, or secret references; only `NEXT_PUBLIC_API_URL`. Go uses the AWS default chain/instance role. Caveat: JWT is stored in `localStorage` (XSS-exposed) and the login page accepts a pasted JWT. `.env` is gitignored and untracked (VERIFIED). |
| 5 | Artifact access tenant-scoped? | **Reads/deletes by id: yes** (`WHERE id=$1 AND user_id=$2`, commit `956e4e8`). **NO overall** because of G7. Other handlers resolve `GetUserID` (tasks 6, approvals 2, agents 2, worker_definitions 6, orchestrator 3, artifacts 7); `ListTasks` repository accepts a nil user filter (`task_repo.go:168`) — confirm the handler always sets it (NOT VERIFIED beyond grep). |
| 6 | Worker/scheduler endpoints | Protected by shared `X-Worker-Key` (non-constant-time compare); key must be strong and kept out of the browser. Worker claim is cross-tenant by design. |

---

## 7. Hosting & ingress

**PWA (`web/`):** Next.js 16.4.3 (not 15), 10 routes (all static except `/tasks/[id]`). `npm ci && npm run build` succeeded in a scratch copy (VERIFIED, exit 0, with `NEXT_PUBLIC_API_URL` set). Recommended host: **Vercel** (CLI present locally; cost doc assumes Hobby). `NEXT_PUBLIC_API_URL` is baked at build time. No `vercel.json`/Dockerfile exist (not needed on Vercel; the runbook's Docker form is unverified). Hobby tier is non-commercial.

**API ingress while EC2 is stopped:** design (`docs/SERVERLESS_API_DESIGN.md`) = Lambda `provided.al2023` arm64 running the same binary with `GRIFFIN_MODE=api`, behind API Gateway HTTP API, DB via Supabase pooler, EC2 woken by `StartInstances`. **Nothing implements this**; the binary is a plain `ListenAndServe` server. No-code route: **AWS Lambda Web Adapter** layer. Caveats:
1. `POST /tasks` wakes the runner in a detached goroutine (`handlers/tasks.go:94-100`); Lambda freezes after the response, so that wake can be dropped. Reliable path = scheduler tick (synchronous `TriggerWakeup`, `handlers/scheduler.go`) → up to ~60 s latency. Approval responses use the same pattern.
2. Wake cooldown is in-memory per Lambda container (harmless: StartInstances is idempotent, `Running/Pending` short-circuits).
3. Needs G4 (real EC2 manager) or nothing wakes.
4. Ship `bootstrap` + empty `migrations/` (G6); Lambda cwd is `/var/task`.
5. Pool: `MaxConns=25/MinConns=2` per container; fine for one user; use the transaction pooler + `simple_protocol`.

Status: packaging MISSING; design BLOCKED on G4/G6/G8.

**Scheduler:** runbook §5 uses `aws scheduler create-schedule` with target `arn:aws:scheduler:::aws-sdk:http:invoke`; that universal target does not exist as written (NOT VERIFIED, treated as invalid). Use an EventBridge **rule + API destination** (§15). The tick requires `X-Worker-Key`; the connection stores it.

---

## 8. Environment variables (names only)

**Go (`internal/config/config.go`):**
- Required in prod: `ENVIRONMENT=production`, `DATABASE_URL`, `SUPABASE_JWT_SECRET`, `WORKER_API_KEY`, `STORAGE_PROVIDER=s3`, `S3_BUCKET`, `S3_REGION`, `AWS_REGION`, `GRIFFIN_MODE` (`api` on Lambda, `worker` on EC2), `AI_PROVIDER` (`antigravity` on the worker), `SCALE_TO_ZERO_ENABLED`, `EC2_INSTANCE_ID` (API only; after G4).
- Tunables: `PORT`, `LOG_LEVEL`, `LEASE_DURATION_SECONDS`, `HEARTBEAT_INTERVAL_SECONDS`, `REAPER_INTERVAL_SECONDS`, `MAX_CONCURRENT_WORKERS`, `ANTIGRAVITY_BINARY`, `MAX_CONCURRENT_ANTIGRAVITY`, `EXECUTION_TIMEOUT_SECONDS`, `GRACEFUL_TERMINATION_SECONDS`, `WORKSPACE_ROOT`, `RETAIN_WORKSPACES_ON_FAILURE`, `IDLE_SHUTDOWN_SECONDS`, `PRE_SHUTDOWN_CHECK_INTERVAL_SECONDS`, `RUNNER_WAKEUP_COOLDOWN_SECONDS`, `S3_PRESIGN_EXPIRY_MINUTES`.
- Do **not** set in prod: `AWS_PROFILE`, `S3_ENDPOINT`, `S3_FORCE_PATH_STYLE`.
- Needed after fixes: `CORS_ALLOWED_ORIGINS` (G2), optional `ENABLE_DEV_LOGIN` (G1). `.env`'s `AWS_S3_BUCKET` is ignored by the code.
- Lambda Web Adapter: `AWS_LAMBDA_EXEC_WRAPPER=/opt/bootstrap`, `AWS_LWA_PORT=8080`, `PORT=8080`.
- Runbook-only placeholders: `SUPABASE_DB_HOST`, `SUPABASE_DB_PASSWORD`, `AGY_REFRESH_TOKEN` (the latter unsupported, §4).
- Web (build time, Vercel): `NEXT_PUBLIC_API_URL`; after Supabase login is added: `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_ANON_KEY` (public by design; never the service-role key).
- Antigravity on EC2 (only if API-key fallback): `GEMINI_API_KEY`, or `AGY_ADC_AUTH`.

---

## 9. CI status (GitHub Actions)

`gh` authenticated as `adhyan-jain` (scopes repo, workflow). Latest runs on `master`: all **completed/success**; the most recent, run `41407208742`, is for head SHA `55c934ac6b946b4771102485c28714c639dce318` = current local and remote HEAD. Jobs: *Test & Build* success, *Code Quality & Static Analysis* success (VERIFIED via `gh run view`). Scope limits: Go only (gofmt, vet, `go test ./...` against a Postgres 17 service); **no web build/lint, no secret scan, no deploy workflow, no arm64 build**. The S3 live test is skipped in CI (needs `GRIFFIN_ENABLE_LIVE_S3_TEST=true`). Locally verified in addition: `go vet ./...` clean, linux/arm64 build OK, web production build OK. Local `go test` was not re-run in this preflight and is not offered as CI evidence.

---

## 10. Supabase migrations

Files (apply in order; down files are destructive): `000001_init_schema`, `000002_rls_policies`, `000003_orchestration_and_agent_factory`, `000004_s3_storage_artifacts`. All use `IF NOT EXISTS` / `DROP POLICY IF EXISTS`, so re-application is safe **except** the auth shim in 000001 (G6). Effects on Supabase: 3 enums, tables, indexes, FORCE RLS + one `auth.uid()=user_id` policy per table.

Real Supabase signup creates `auth.users` rows but **no `profiles`/`workspaces` rows** (no trigger in the migrations; only the dev login inserted profiles). No non-test Go code reads `profiles`, so this is harmless today.

Connection strings (confirm exact hosts in dashboard → Connect; direct `db.<ref>.supabase.co` is IPv6-only unless the IPv4 add-on is bought, so use poolers — NOT VERIFIED for your project):
- Migrations from your laptop and the EC2 worker (long-lived, needs `FOR UPDATE SKIP LOCKED`): **session pooler**, port 5432, user `postgres.<project-ref>`.
- Lambda: **transaction pooler**, port 6543, append `?default_query_exec_mode=simple_protocol`.

```bash
# run from repo root; $DATABASE_URL_SESSION comes from a chmod-600 env file, never echoed
SCR=/tmp/claude-1000/-home-adhyan-Desktop-griffin/4bc137a7-7053-4a1d-8134-285ed1b446c2/scratchpad
sed '/^-- Compatibility shim/,/^\$\$ LANGUAGE SQL STABLE;/d' migrations/000001_init_schema.up.sql > $SCR/000001_supabase.up.sql
diff migrations/000001_init_schema.up.sql $SCR/000001_supabase.up.sql    # expect only lines 8-21 removed (VERIFIED)

pg_dump --schema-only "$DATABASE_URL_SESSION" > $SCR/pre-migration-schema.sql   # rollback baseline
for f in $SCR/000001_supabase.up.sql migrations/000002_rls_policies.up.sql \
         migrations/000003_orchestration_and_agent_factory.up.sql migrations/000004_s3_storage_artifacts.up.sql; do
  psql "$DATABASE_URL_SESSION" -v ON_ERROR_STOP=1 --single-transaction -f "$f" || break
done
psql "$DATABASE_URL_SESSION" -Atc "select count(*) from information_schema.tables where table_schema='public'"
psql "$DATABASE_URL_SESSION" -Atc "select relname from pg_class where relrowsecurity and relforcerowsecurity and relnamespace='public'::regnamespace order by 1"
```
Supabase dashboard: Auth → disable public sign-ups (single operator) and create the operator user; Settings → JWT Keys: record whether HS256 legacy secret or asymmetric (G8); put the JWT secret in the 600 env file.

---

## 11. Deployment commands in execution order

Conventions: run from repo root; `P="--profile griffin-bootstrap --region ap-south-1"`; secrets live in `~/.griffin-prod.env` (`chmod 600`, outside the repo) and are loaded with `set -a; . ~/.griffin-prod.env; set +a`. Never `echo` them.

**Phase 0 — close gates G1-G8** (section 3). Re-run CI; confirm green on the new SHA (`gh run watch`). **Do not continue until done.**

**Phase 1 — local prerequisites**
```bash
yay -S aws-session-manager-plugin     # or install per AWS docs
session-manager-plugin --version
aws sts get-caller-identity --profile griffin-bootstrap
```

**Phase 2 — IAM (admin profile):** attach Policy A (§5.3). Verify:
```bash
aws ec2 describe-instances $P --max-items 1 >/dev/null && echo OK
aws ec2 run-instances $P --dry-run --image-id ami-00000000 --instance-type t4g.medium 2>&1 | head -2  # expect DryRun*/InvalidAMI, not UnauthorizedOperation
```

**Phase 3 — Supabase:** project + migrations (§10).

**Phase 4 — secrets in SSM** (needs Policy B `Secrets`; values come from env vars, never echoed)
```bash
set -a; . ~/.griffin-prod.env; set +a
put() { aws ssm put-parameter $P --type SecureString --overwrite --name "$1" --value "$2" >/dev/null && echo "stored $1"; }
put /griffin/prod/worker/DATABASE_URL        "$DATABASE_URL_SESSION"
put /griffin/prod/worker/SUPABASE_JWT_SECRET "$SUPABASE_JWT_SECRET"
put /griffin/prod/worker/WORKER_API_KEY      "$WORKER_API_KEY"      # >=32 random chars: openssl rand -base64 36
```

**Phase 5 — EC2** (§13). **Phase 6 — Antigravity auth** (§14). **Phase 7 — API** (§15). **Phase 8 — PWA** (§16). **Phase 9 — DNS/TLS** (§17). **Phase 10 — verification** (§20).

---

(IAM policies are in §5; Antigravity auth is in §4 and §14.)

## 13. EC2 bootstrap procedure

Run the idempotency guard (§5.1) first.

```bash
# 1. AMI (no DescribeImages needed)
AMI_ID=$(aws ssm get-parameter $P --name /aws/service/canonical/ubuntu/server/24.04/stable/current/arm64/hvm/ebs-gp3/ami-id --query Parameter.Value --output text)
aws ec2 describe-images $P --image-ids "$AMI_ID" --query 'Images[0].[Name,Architecture]' --output text   # expect arm64

# 2. Provision (script as-is is acceptable once the guard is clean). Record INSTANCE_ID.
AWS_PROFILE=griffin-bootstrap ./infra/provision-ec2.sh
#    If the script printed the Notice about falling back to the hard-coded AMI, stop and fix the AMI first.
aws ec2 modify-instance-metadata-options $P --instance-id $INSTANCE_ID --http-tokens required --http-put-response-hop-limit 1
aws ec2 modify-instance-attribute $P --instance-id $INSTANCE_ID --instance-initiated-shutdown-behavior stop
# 3. Replace the broad S3 inline policy with the §5.5 worker policy
aws iam put-role-policy $P --role-name GriffinEC2WorkerRole --policy-name GriffinS3ArtifactAccess --policy-document file://worker-policy.json
# 4. Wait for SSM registration and check cloud-init
aws ssm describe-instance-information $P --filters Key=InstanceIds,Values=$INSTANCE_ID --query 'InstanceInformationList[0].PingStatus'
aws ec2 get-console-output $P --instance-id $INSTANCE_ID --latest --output text | tail -20
```

On the instance (`aws ssm start-session --target $INSTANCE_ID $P`, then `sudo -i`):
```bash
cloud-init status --long; id griffin; loginctl show-user griffin -p Linger
apt-get install -y dbus-user-session
su - griffin -c 'agy --version && uname -m'               # must print aarch64

# AWS CLI v2 (arm64) for the env loader
cd /tmp && curl -fsSL https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip -o awscliv2.zip && unzip -q awscliv2.zip && ./aws/install

# Griffin binary + EMPTY migrations dir (G6)
install -d -o griffin -g griffin /opt/griffin /opt/griffin/migrations
install -d -o root -g griffin -m 750 /etc/griffin
touch /opt/griffin/migrations/.keep
# Build locally: GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o griffin-server ./cmd/griffin-server
# Transfer via S3 (aws s3 cp ... s3://<bucket>/releases/<sha>/griffin-server) then:
#   aws s3 cp s3://<bucket>/releases/<sha>/griffin-server /opt/griffin/griffin-server
chown griffin:griffin /opt/griffin/griffin-server; chmod 755 /opt/griffin/griffin-server

# Allow the idle monitor to power off (G5)
command -v shutdown            # normally /usr/sbin/shutdown
echo 'griffin ALL=(root) NOPASSWD: /usr/sbin/shutdown -h now' > /etc/sudoers.d/griffin-shutdown
chmod 440 /etc/sudoers.d/griffin-shutdown && visudo -cf /etc/sudoers.d/griffin-shutdown

# Env loader (root; refreshes /etc/griffin/worker.env from SSM)
cat >/usr/local/bin/griffin-fetch-env.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
umask 027
{
  aws ssm get-parameters-by-path --region ap-south-1 --path /griffin/prod/worker --with-decryption \
    --query 'Parameters[].[Name,Value]' --output text | awk -F'\t' '{n=$1; sub(".*/","",n); print n"="$2}'
  cat <<CFG
ENVIRONMENT=production
GRIFFIN_MODE=worker
AI_PROVIDER=antigravity
ANTIGRAVITY_BINARY=/home/griffin/.local/bin/agy
WORKSPACE_ROOT=/var/griffin/workspaces
SCALE_TO_ZERO_ENABLED=true
IDLE_SHUTDOWN_SECONDS=300
STORAGE_PROVIDER=s3
S3_BUCKET=griffin-artifacts-220115213279-ap-south-1
S3_REGION=ap-south-1
AWS_REGION=ap-south-1
MAX_CONCURRENT_WORKERS=2
MAX_CONCURRENT_ANTIGRAVITY=1
CFG
} > /etc/griffin/worker.env
chown root:griffin /etc/griffin/worker.env; chmod 0640 /etc/griffin/worker.env
EOF
chmod 750 /usr/local/bin/griffin-fetch-env.sh
/usr/local/bin/griffin-fetch-env.sh     # run once now so the file exists

cat >/etc/systemd/system/griffin-worker.service <<'EOF'
[Unit]
Description=Griffin worker (scale-to-zero runner)
After=network-online.target local-fs.target user@1001.service
Wants=network-online.target
Requires=user@1001.service

[Service]
User=griffin
WorkingDirectory=/opt/griffin
Environment=HOME=/home/griffin XDG_RUNTIME_DIR=/run/user/1001
Environment=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1001/bus
Environment=PATH=/home/griffin/.local/bin:/usr/local/bin:/usr/bin:/bin
ExecStartPre=+/usr/local/bin/griffin-fetch-env.sh
EnvironmentFile=-/etc/griffin/worker.env
ExecStart=/opt/griffin/griffin-server
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
# DO NOT enable/start yet: authenticate agy first (§14)
```
Caveat: systemd reads `EnvironmentFile` before `ExecStartPre` runs, so the fetch refreshes the file for the *next* start; after rotating a secret, restart the service twice. This unit and loader are untested drafts (NOT VERIFIED).

Expected idle behavior: the monitor runs only with `GRIFFIN_MODE=worker` and scale-to-zero on; after 300 s with an empty queue → drain → recheck → `sudo shutdown -h now` → state `stopped`.

---

## 14. Antigravity authentication procedure (on the instance)

```bash
# SSM session, then:
sudo -i -u griffin
export PATH="$HOME/.local/bin:$PATH" XDG_RUNTIME_DIR=/run/user/$(id -u) DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u)/bus
agy --version; agy --help | grep -E 'output-format|print'     # confirm flags exist in this version
agy                      # prints auth URL -> open on laptop, sign in, paste code
agy -p "Reply with the JSON {\"status\":\"ready\"}" --output-format json ; echo "exit=$?"
ls -la ~/.gemini/antigravity-cli/                              # note where state lives (names only)
```
Persistence tests (all required before enabling the service):
1. Exit all sessions; as root: `sudo -u griffin env XDG_RUNTIME_DIR=/run/user/1001 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1001/bus HOME=/home/griffin /home/griffin/.local/bin/agy -p ping --output-format json` → must succeed.
2. `sudo reboot`; after boot with **no** SSM shell open, run the same via `aws ssm send-command --document-name AWS-RunShellScript` → must succeed. If it asks to re-authenticate: keyring locked (R1); fall back to `GEMINI_API_KEY` mode (cost implications) or find a documented file-backed store.
3. `aws ec2 stop-instances` / `start-instances` cycle, repeat.
4. Only then: `systemctl enable --now griffin-worker && journalctl -u griffin-worker -n 50`.

---

## 15. API deployment (Lambda + HTTP API)

```bash
# Build + package (Phase-0 fixes included)
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o build/bootstrap ./cmd/griffin-server
mkdir -p build/migrations && touch build/migrations/.keep
(cd build && zip -qr ../griffin-api.zip bootstrap migrations)

# Verify the adapter layer before relying on it (ARN from memory, NOT VERIFIED)
aws lambda list-layer-versions $P --layer-name arn:aws:lambda:ap-south-1:753240598075:layer:LambdaAdapterLayerArm64 --query 'LayerVersions[0].LayerVersionArn' --output text
#   -> export LWA_LAYER_ARN=<that value>; if empty/denied, take the current ARN from the awslabs/aws-lambda-web-adapter README

# Role
aws iam create-role $P --role-name griffin-api-lambda-role --assume-role-policy-document '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}'
aws iam attach-role-policy $P --role-name griffin-api-lambda-role --policy-arn arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole
aws iam put-role-policy $P --role-name griffin-api-lambda-role --policy-name griffin-api --policy-document file://api-policy.json   # §5.5 with INSTANCE_ID filled in

# Function (env built from the 600 env file via jq so values never print)
set -a; . ~/.griffin-prod.env; set +a
ENVJSON=$(jq -n --arg db "$DATABASE_URL_TXN" --arg jwt "$SUPABASE_JWT_SECRET" --arg wk "$WORKER_API_KEY" \
  --arg iid "$INSTANCE_ID" --arg cors "https://$WEB_DOMAIN" \
  '{Variables:{ENVIRONMENT:"production",GRIFFIN_MODE:"api",DATABASE_URL:$db,SUPABASE_JWT_SECRET:$jwt,WORKER_API_KEY:$wk,
    STORAGE_PROVIDER:"s3",S3_BUCKET:"griffin-artifacts-220115213279-ap-south-1",S3_REGION:"ap-south-1",
    SCALE_TO_ZERO_ENABLED:"true",EC2_INSTANCE_ID:$iid,CORS_ALLOWED_ORIGINS:$cors,
    PORT:"8080",AWS_LWA_PORT:"8080",AWS_LAMBDA_EXEC_WRAPPER:"/opt/bootstrap"}}')
aws lambda create-function $P --function-name griffin-api --runtime provided.al2023 --architectures arm64 \
  --handler bootstrap --zip-file fileb://griffin-api.zip --timeout 29 --memory-size 256 \
  --role arn:aws:iam::220115213279:role/griffin-api-lambda-role --layers "$LWA_LAYER_ARN" --environment "$ENVJSON"
```
(`AWS_REGION` is set automatically by Lambda to `ap-south-1`; do not set it.)

HTTP API + custom domain:
```bash
API_ID=$(aws apigatewayv2 create-api $P --name griffin-api --protocol-type HTTP --target arn:aws:lambda:ap-south-1:220115213279:function:griffin-api --query ApiId --output text)
aws lambda add-permission $P --function-name griffin-api --statement-id apigw --action lambda:InvokeFunction --principal apigateway.amazonaws.com --source-arn "arn:aws:execute-api:ap-south-1:220115213279:$API_ID/*"
CERT_ARN=$(aws acm request-certificate $P --domain-name "$API_DOMAIN" --validation-method DNS --query CertificateArn --output text)
aws acm describe-certificate $P --certificate-arn $CERT_ARN --query 'Certificate.DomainValidationOptions[0].ResourceRecord'   # create this CNAME at your DNS host; wait for ISSUED
aws apigatewayv2 create-domain-name $P --domain-name "$API_DOMAIN" --domain-name-configurations CertificateArn=$CERT_ARN
aws apigatewayv2 create-api-mapping $P --domain-name "$API_DOMAIN" --api-id $API_ID --stage '$default'
aws apigatewayv2 get-domain-name $P --domain-name "$API_DOMAIN" --query 'DomainNameConfigurations[0].ApiGatewayDomainName'  # CNAME target
```
(`apigatewayv2` calls map to the `apigateway:*` permissions in Policy B.)

Scheduler tick (EventBridge rule → API destination):
```bash
CONN_ARN=$(aws events create-connection $P --name griffin-tick --authorization-type API_KEY \
  --auth-parameters "ApiKeyAuthParameters={ApiKeyName=X-Worker-Key,ApiKeyValue=$WORKER_API_KEY}" --query ConnectionArn --output text)
DEST_ARN=$(aws events create-api-destination $P --name griffin-tick --connection-arn $CONN_ARN \
  --invocation-endpoint "https://$API_DOMAIN/api/v1/scheduler/tick" --http-method POST --invocation-rate-limit-per-second 1 --query ApiDestinationArn --output text)
aws iam create-role $P --role-name griffin-events-invoke-role --assume-role-policy-document '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"events.amazonaws.com"},"Action":"sts:AssumeRole"}]}'
aws iam put-role-policy $P --role-name griffin-events-invoke-role --policy-name invoke --policy-document "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":\"events:InvokeApiDestination\",\"Resource\":\"$DEST_ARN\"}]}"
aws events put-rule $P --name griffin-tick --schedule-expression 'rate(1 minute)' --state DISABLED   # enable after verification
aws events put-targets $P --rule griffin-tick --targets "Id=1,Arn=$DEST_ARN,RoleArn=arn:aws:iam::220115213279:role/griffin-events-invoke-role"
```
S3 lifecycle (stops versioned storage growing):
```bash
aws s3api put-bucket-lifecycle-configuration --bucket griffin-artifacts-220115213279-ap-south-1 $P --lifecycle-configuration \
 '{"Rules":[{"ID":"expire-noncurrent","Status":"Enabled","Filter":{},"NoncurrentVersionExpiration":{"NoncurrentDays":30},"AbortIncompleteMultipartUpload":{"DaysAfterInitiation":7}},{"ID":"expire-delete-markers","Status":"Enabled","Filter":{},"Expiration":{"ExpiredObjectDeleteMarker":true}}]}'
```
Everything in this section is NOT VERIFIED until executed.

---

## 16. PWA deployment (Vercel)

```bash
cd web
vercel link                                     # root dir = web
vercel env add NEXT_PUBLIC_API_URL production   # value: https://$API_DOMAIN (typed at prompt)
# after Supabase login is implemented:
vercel env add NEXT_PUBLIC_SUPABASE_URL production
vercel env add NEXT_PUBLIC_SUPABASE_ANON_KEY production
vercel deploy --prod
vercel domains add "$WEB_DOMAIN"
```
Gate: the deployed bundle must not contain "Quick Dev Login": `curl -s https://$WEB_DOMAIN/login | grep -c "Quick Dev"` → 0 (the page is client-rendered; also grep the built JS chunks). Set `CORS_ALLOWED_ORIGINS` to exactly `https://$WEB_DOMAIN`.

## 17. DNS / HTTPS requirements
- Two real hostnames you own (`API_DOMAIN`, `WEB_DOMAIN`) — currently BLOCKED (placeholders only).
- API: ACM public cert in **ap-south-1** (DNS-validated CNAME) → API Gateway custom domain → CNAME to the regional API Gateway domain name.
- Web: Vercel-issued TLS; add the DNS record Vercel prints.
- No inbound rules on the EC2 SG; no tunnel needed with this design.
- HTTPS only.

## 18. Rollback

| Layer | Action |
|---|---|
| PWA | `vercel rollback` (or promote a prior deployment) |
| API | `aws lambda update-alias` to a prior published version (publish versions each release) or `update-function-code` with the previous zip. Emergency stop: `aws events disable-rule --name griffin-tick` and `aws lambda put-function-concurrency --function-name griffin-api --reserved-concurrent-executions 0` |
| Scheduler | `aws events disable-rule --name griffin-tick` |
| Worker | `systemctl disable --now griffin-worker`; `aws ec2 stop-instances`; restore previous binary from `s3://…/releases/<prev-sha>/` |
| Instance (full) | `terminate-instances`, then **delete the retained volume** (`DeleteOnTermination:false`); remove SG/role/profile if abandoning |
| DB | Restore from Supabase backups / the `pre-migration-schema.sql` baseline (Free tier has no PITR). `*.down.sql` drop everything — never run on prod without a data dump |
| Secrets | Rotate `WORKER_API_KEY`/`SUPABASE_JWT_SECRET` (SSM put + Lambda update + connection update, together) |
| Dev login leaked | Rotate the JWT secret immediately (invalidates all tokens) |

---

## 19. Cost estimate & scale-to-zero assumptions

Pricing could not be queried (`pricing:GetProducts` denied) → **all prices NOT VERIFIED**. The repo's figures (`t4g.medium` $0.0224/h, gp3 $0.08/GB-mo) look low versus typical published rates for this region (my estimate: ~$0.035-0.040/h, gp3 ~$0.09/GB-mo); confirm in the AWS Pricing Calculator.

Assumptions: 60 runner-hours/month (2 h/day), instance stops after 5 min idle, 30 GiB gp3 persists, public IPv4 only while running ($0.005/h), 1 tick/min (43,200/mo), single user, small artifact volume.

| Item | Repo figure | Preflight estimate |
|---|---|---|
| EC2 t4g.medium, 60 h | $1.34 | $2.1-2.4 |
| EBS 30 GiB gp3 (always billed) | $2.40 | $2.4-2.8 |
| Public IPv4 (60 h) | $0.30 | $0.30 |
| Lambda + HTTP API (~45k req) | <$0.10 | ~$0.05-0.10 |
| EventBridge rule → API destination | $0.04 | <$0.10 |
| CloudWatch Logs, S3 (versioned, lifecycle 30 d) | — | $0.10-0.50 |
| ACM / SSM standard params | $0 | $0 |
| **AWS total** | **~$4.13** | **~$5-7 / month** |
| Supabase | — | Free ($0) or Pro $25; Free projects pause after inactivity (ticks keep it active) |
| Vercel Hobby | $0 | $0 (non-commercial terms) |
| Gemini key (only if API-key fallback) | — | usage-based |

Failure-mode cost: if idle shutdown fails (G5) or the instance is left running ≈ 730 h ≈ $26-29 + EBS/IPv4 ≈ **$33/month**; an orphaned volume after terminate ≈ $2.7/month. Create a Budget with alerts (e.g. $10/$20). Scale-to-zero only holds if G4 + G5 are fixed and `IDLE_SHUTDOWN_SECONDS` stays ≤ 300.

---

## 20. Live verification checklist

Run in order; each must pass before the next.

| # | Check | Command / expectation | Status |
|---|---|---|---|
| 1 | CI green on deploy SHA | `gh run list --limit 1` → success, SHA matches | current HEAD VERIFIED; new SHA NOT VERIFIED |
| 2 | Policy A effective | `aws ec2 describe-instances …` OK | NOT VERIFIED |
| 3 | Bucket posture | encryption, BPA, ownership (re-run §1 checks) | VERIFIED today |
| 4 | Migrations applied | all tables present, `relforcerowsecurity` true | NOT VERIFIED |
| 5 | Instance launched, SSM `Online`, no inbound | `describe-security-groups` shows 0 ingress | NOT VERIFIED |
| 6 | EBS encrypted | `describe-volumes --query 'Volumes[].Encrypted'` → true | NOT VERIFIED |
| 7 | agy on aarch64; auth survives session end, reboot, stop/start | §14 tests 1-3 | NOT VERIFIED |
| 8 | Health | `curl -fsS https://$API_DOMAIN/healthz` and `/readyz` | NOT VERIFIED |
| 9 | Auth enforced | `curl -i https://$API_DOMAIN/api/v1/tasks` → 401; `curl -i -X POST https://$API_DOMAIN/api/v1/auth/login -d '{"email":"x@y.z"}'` → 404/disabled | NOT VERIFIED (currently FAILS, G1) |
| 10 | CORS | `curl -si -H 'Origin: https://evil.example' -X OPTIONS https://$API_DOMAIN/api/v1/tasks` → no `Access-Control-Allow-Origin`; real web origin → echoed exactly | NOT VERIFIED (currently FAILS, G2) |
| 11 | Real Supabase login in PWA → task create returns 202 with `X-Runner-Status: WAKEUP_SIGNALED`; no `service_role`/`AKIA` strings in built JS | browser + DevTools, `grep -R` on `.next` | NOT VERIFIED |
| 12 | Wake-up | instance `stopped` → create task → `running` within ~60 s | NOT VERIFIED (G4) |
| 13 | Task executes | task reaches `COMPLETED`; `journalctl -u griffin-worker` shows agy run; attempts recorded | NOT VERIFIED |
| 14 | Tenant isolation | second Supabase user cannot GET/presign/delete user A's artifact id, nor register A's `object_key` via `POST /artifacts` | NOT VERIFIED (G7) |
| 15 | Artifact round trip | upload (<6 MB) → S3 object (SSE AES256) → presigned download → delete | NOT VERIFIED |
| 16 | Scale-to-zero | after queue empty + 5 min, instance state `stopped` | NOT VERIFIED (G5) |
| 17 | Tick | enable rule; Lambda logs show `/scheduler/tick` 200 each minute; wrong `X-Worker-Key` → 401 | NOT VERIFIED |
| 18 | Auth expiry handling | log agy out in a test window → task parks with auth error, no silent fallback | NOT VERIFIED |
| 19 | Cost | Budget exists; after 24 h EC2 hours ≈ active hours | NOT VERIFIED |
| 20 | Rollback drill | disable rule, prior Lambda alias, `vercel rollback` all work | NOT VERIFIED |

---

## Appendix — what was and was not executed in this preflight
Executed (read-only): file reads; `aws sts/s3api/ec2/iam/scheduler/lambda/ssm/pricing` read calls (several denied, listed above); `gh run list/view`; `git log/ls-remote`; local `agy --help`/`--version`; `curl` of the public agy installer script and two public manifests (installer **not executed**); fetch of the official Antigravity install docs; `go vet`, linux/arm64 `go build`, `npm ci && npm run build` in a scratch copy; `sed`/`diff` of migration 000001 into scratch.
Not executed: any AWS write or dry-run write, Supabase access, deployment, IAM change, secret read. One attempted probe of an unrelated AWS profile was blocked by the harness and was out of scope; nothing from it was used.
