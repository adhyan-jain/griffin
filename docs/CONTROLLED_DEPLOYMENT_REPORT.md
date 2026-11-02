# Controlled deployment report

Repo HEAD at start: `47ad767` (clean). **Nothing was deployed or mutated in AWS, Supabase or Vercel.** AWS access this session
was read-only (`griffin-bootstrap`). Status vocabulary: VERIFIED · IMPLEMENTED_NOT_LIVE_VERIFIED · BLOCKED · REQUIRES_USER_APPROVAL.

## Phase A — live read-only findings (VERIFIED, 2026-11-02)
| Item | Result |
|---|---|
| Identity / region | user `griffin-bootstrap`, account `220115213279`, `ap-south-1` |
| Bucket `griffin-artifacts-220115213279-ap-south-1` | in ap-south-1; public access fully blocked; SSE AES256 (SSE-C blocked); **versioning on**; **no lifecycle rule** |
| `GriffinEC2WorkerRole` / `GriffinEC2WorkerProfile` | exist, profile contains the role, trust = ec2. Inline `GriffinArtifactStorageAccess` (S3 only) + `AmazonSSMManagedInstanceCore`. **No SSM-parameter access yet** |
| Default VPC / subnets | `vpc-0c233c7991952ec37`; 3 public default subnets (1a/1b/1c) |
| EC2 inventory | none |
| Ubuntu 24.04 arm64 AMI | resolvable via `ec2:DescribeImages` (latest seen `ami-072d8688e7b3aba7a`); the SSM public-parameter read is denied |
| `griffin-bootstrap` denials | RunInstances (per earlier audit), SSM, Lambda, API Gateway, EventBridge, Budgets, IAM-user reads, Access Analyzer, DescribeInstanceTypeOfferings |
| Lambda Web Adapter layer | arm64 `arn:aws:lambda:ap-south-1:753240598075:layer:LambdaAdapterLayerArm64:<ver>`, `AWS_LAMBDA_EXEC_WRAPPER=/opt/bootstrap` — matches upstream README (docs-verified, not live) |

## Defects found and fixed in this pass (code/infra, not yet committed unless noted in git log)
1. `infra/provision-ec2.sh` called `iam get-role` then `create-role`; with the deployer lacking IAM read it would fail/try to recreate the role. Now **no IAM calls**; uses the existing profile. Added read-only `GRIFFIN_DRY_RUN=1` (uses `run-instances --dry-run`, creates nothing).
2. `infra/user-data.sh`: removed a second inline systemd unit that conflicted with `infra/systemd/griffin-worker.service` (wrong binary path); removed `apt install amazon-ssm-agent` (snap on Ubuntu 24.04 AMIs; would abort cloud-init under `set -e`) — IMPLEMENTED_NOT_LIVE_VERIFIED.
3. Worker env now sets `DB_MAX_CONNS=5 DB_MIN_CONNS=0` (default was 25 through a session pooler).
4. Docs suggested `GEMINI_API_KEY` as an automatic fallback (per-token billing). Rewritten to STOP-and-report. Code never reads that key.
5. IAM drafts replaced by `docs/FINAL_IAM_POLICIES/` (A, B, C, D, E). Earlier drafts had no deployer permissions for Lambda/API Gateway/EventBridge and conflicting "Policy B" definitions.

## Open decisions for you
- **`POST /api/v1/runner/sleep` is callable by any authenticated user and stops the shared runner** (`server.go:156`; UI buttons in `RunnerPill.tsx`, settings page). Fine for a single operator with sign-ups disabled; a cross-tenant DoS otherwise. Policy B omits `StopInstances`, so the button would fail in production. Recommended: hide the button / make the route worker-key-only. Not changed without your say-so.
- Supabase plan tier (backups), asymmetric vs legacy JWT keys, alert email, whether to use a `*.vercel.app` URL (needed before the Lambda `CORS_ALLOWED_ORIGINS` can be set).

## Blockers
| ID | Blocker | Status |
|---|---|---|
| B1 | IAM A (and D later) not attached; C not added to worker role | REQUIRES_USER_APPROVAL (admin) |
| B2 | No Supabase project / credentials in the environment (no `~/.griffin-prod.env`, no web env) | BLOCKED (user action) |
| B3 | Antigravity unattended auth across restart/reboot/stop-start | BLOCKED until instance exists; gate defined in `CLOUD_ACCEPTANCE_TESTS.md` |
| B4 | S3 lifecycle (`infra/ops/s3-lifecycle.json`) | IMPLEMENTED_NOT_LIVE_VERIFIED; needs approval + Policy D |
| B5 | Budgets $10/$20, alarms (`infra/ops/create-*.sh`, dry-run by default) | IMPLEMENTED_NOT_LIVE_VERIFIED; needs approval + alert email |
| B6 | Lambda / HTTP API / EventBridge tick | IMPLEMENTED_NOT_LIVE_VERIFIED (commands in preflight §15 are valid AWS shapes); REQUIRES_USER_APPROVAL |
| B7 | PWA on Vercel (CLI logged in as `adhyan-jain`, project not linked) | REQUIRES_USER_APPROVAL; useless until B2 and API exist |

## Approval checkpoints (nothing proceeds without your explicit yes)
Each lists resources / permissions / cost / rollback / risks. Rollback for all: `docs/ROLLBACK_PROCEDURE.md`.
1. **Attach IAM** — admin attaches A to `griffin-bootstrap`, C to the worker role. Cost $0. Risk: A grants launch of one `t4g.medium` and Session Manager only; unvalidated by AWS (run `validate-policy` first).
2. **Supabase** — create project (ap-south-1), disable sign-ups, run `griffin-migrate up` (4 migrations) after `pg_dump`. Cost: plan-dependent (free tier possible). Risk: untested against hosted Supavisor.
3. **EC2** — `GRIFFIN_DRY_RUN=1 ./infra/provision-ec2.sh` first (free), then real run: 1× t4g.medium, 30 GiB encrypted gp3, SG `griffin-runner-sg` (zero inbound), existing profile. Cost: ~$0.035/h running (estimate) + ~$2.7/mo EBS while stopped.
4. **Lambda / HTTP API / EventBridge** — D attached; `griffin-api` (arm64, 256 MB, 29 s), HTTP API, rule `griffin-tick` (starts DISABLED), connection/destination (Secrets Manager secret ≈ $0.40/mo). Cost ≈ <$1/mo at 1 tick/min (43,200 req/mo) plus secret.
5. **Ongoing-charge resources** — budgets, SNS, alarms, S3 lifecycle (free/cents).
6. **Public exposure** — Vercel deploy and API Gateway URL.

## Cost (ESTIMATES from list-price memory, NOT AWS bills; Pricing API denied)
- Scale-to-zero, ~2 h/day runtime: compute ≈ 60 h × $0.035 ≈ $2; EBS 30 GiB ≈ $2.7; connection secret $0.40; API/Lambda/S3/logs ≈ $1 → **≈ $6–7/mo** (under the $10 alert).
- Worst case, runner 24×7: compute ≈ 730 h × $0.035 ≈ $26; EBS $2.7; public IPv4 ≈ $3.7; other ≈ $1.5 → **≈ $34/mo** (breaches both budgets; the forecast alert would fire).

## Verification this pass
Shell syntax (`bash -n`), JSON validity of all policy files, `go test ./internal/runner` (infra/systemd/sudoers/env-script coherence) — see git log/commit for the final run. Full DB-backed suite not re-run (no code paths changed outside infra scripts).
