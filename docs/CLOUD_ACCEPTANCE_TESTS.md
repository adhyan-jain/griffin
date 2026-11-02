# Cloud acceptance tests

**Nothing below has been run.** Status of every test: **REQUIRES_USER_APPROVAL** (needs the infrastructure from checkpoints 1–6).
A test only counts as passed when run against the real service; local/mock runs are listed separately in the report.
`P="--profile griffin-bootstrap --region ap-south-1"`; `$I` = runner instance id.

## Part 1 — Antigravity authentication gate (Phase D). The worker service stays DISABLED until T6 passes.
Only supported auth is used: interactive URL/code login on first `agy` run. No token injection, cookie extraction, keyring
unlocking tricks, or `GEMINI_API_KEY`/ADC (those are per-token / GCP-billed alternatives, see "If it fails").

| ID | Test | Pass criterion |
|---|---|---|
| T0 | On the instance as `griffin`: `agy --version; agy --help` | Version recorded; `-p`, `--output-format json`, `--print-timeout` present (local reference: 1.2.0; the installer currently serves 1.4.12 — pin and record the installed one) |
| T1 | `sudo -i -u griffin`, export `XDG_RUNTIME_DIR=/run/user/1001 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1001/bus`, run `agy`, complete URL/code login | Interactive session reaches the prompt |
| T2 | `agy -p 'Reply with {"status":"ready"}' --output-format json; echo $?` | exit 0 |
| T3 | Same with `--json-schema` and parse with `jq` | valid JSON matching schema |
| T4 | Restart: close SSM session; `aws ssm send-command` (AWS-RunShellScript) running T2 as `griffin` with the env above | exit 0, **no login prompt** |
| T5 | `sudo reboot`; wait for SSM ping; repeat T4 with no session attached | exit 0 |
| T6 | `aws ec2 stop-instances` → wait stopped → `start-instances` → repeat T4 | exit 0 |

If T4–T6 produce an authentication prompt/`ErrAuthenticationRequired`: **STOP.** Do not enable `griffin-worker`, do not add an
API key. Report to the user the legitimate alternatives and their cost: (a) re-login manually after each boot (not unattended);
(b) keep the instance running 24/7 so the keyring session survives (≈ continuous-running cost in the report; still re-auth after reboot);
(c) self-host the worker on a machine the user controls with subscription auth; (d) ADC / `GEMINI_API_KEY` — **per-usage billing, needs the user's explicit decision**.

## Part 2 — End-to-end (after T6 passes and the worker is enabled)
| ID | Test | Pass criterion |
|---|---|---|
| E1 | Sign in to the PWA (Supabase), submit a task | Task row `queued`; PWA shows it |
| E2 | Observe PWA → JJ → Manager → Specialist → `agy` → result | Task `completed`; result visible in PWA; row in Postgres; artifact object under `users/<uid>/` in S3 |
| E3 | Shutdown survival: queue a task while instance is `stopped` | Task stays durable; instance starts (≤ ~60 s); task completes |
| E4 | Kill power mid-task (`stop-instances`) | Lease expires; reaper requeues; task resumes after next start; no duplicate completion |
| E5 | Failure recovery: make `agy` fail once (e.g., invalid model via a test task) | Task marked failed/retried per policy; visible and re-runnable |
| E6 | Approval gate: task with an approval-required action | Stays blocked (no execution, no artifact) until approved in PWA; then proceeds |
| E7 | Duplicate wake: fire `POST /runner/wake` ×5 quickly while stopped | One `StartInstances` effect; no error storm; cooldown honoured |
| E8 | Idle shutdown: leave queue empty | Instance `stopped` ~300 s after last work; **not** while any task is `running`/`queued` |
| E9 | Wake after shutdown: submit a new task | Instance starts again; task completes |
| E10 | Tenant isolation (two users): B requests A's task/artifact | 404/403; no data returned |
| E11 | Security smoke: `POST /api/v1/auth/login` | 404; evil `Origin` gets no CORS headers; `GET /runner/status` unauthenticated → 401 |
| E12 | Scheduler: `aws events enable-rule`; check `/scheduler/tick` hits in Lambda logs; disable after | tick every minute, 2xx; wake only when runnable work exists |

Cost/ops checks after deploy: budgets present (`aws budgets describe-budgets`), alarms in `OK`, S3 lifecycle returned by
`get-bucket-lifecycle-configuration`, Cost Explorer shows EC2/S3/Lambda/API Gateway lines.
