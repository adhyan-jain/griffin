# Integration Status (code + tests only, no live calls)

Levels: CONFIGURED = config/env wiring exists; CONNECTED = production code path constructs a real client;
TESTED = automated test exercises the logic (mocked/fake transport); UNAVAILABLE = not implemented.
No live AWS / Supabase / Antigravity call was made for this audit; "CONNECTED" means wired, not verified live.

| Integration | Level | Evidence |
|---|---|---|
| S3 artifact storage | CONFIGURED, CONNECTED, TESTED (mocked SDK) | `internal/config/config.go:49,56,126,131` (S3_PRESIGN_EXPIRY_MINUTES, MAX_UPLOAD_BYTES); `cmd/griffin-server/main.go:122-142` selects `NewS3StorageProvider` vs mock; `internal/storage/s3_provider.go`; tests `TestS3StorageProvider_Upload/Download/Head/Presign/ErrorMapping`, `TestStorageService_*`, `TestArtifacts_*` (`internal/api/handlers/artifacts_test.go`). Live test `tests/s3_live_test.go:21 TestS3_LiveAWSVerification` is opt-in (`GRIFFIN_ENABLE_LIVE_S3_TEST`), not run. |
| EC2 runner wake / stop | CONFIGURED, CONNECTED, TESTED (fake EC2 API) | `cmd/griffin-server/main.go:157` `NewEC2InstanceManager` when `EC2InstanceID` set; `internal/runner/instance.go:108-210`, `wakeup.go:59`; tests `TestEC2StartRunner`, `TestEC2StopRunner`, `TestWakeupController_*`, `TestIdleShutdown_*`, `TestScaleToZero_TaskSubmissionReturns202AndWakesRunner`. Falls back to MockInstanceManager (`main.go:166-169`) if unset. |
| Supabase auth (HS256 JWT) | CONFIGURED, CONNECTED, TESTED | `internal/api/middleware/auth.go:44`; `TestAuthenticateUserHS256`, `TestAuthenticateUserIssuer`, `TestAuthenticateUserOperatorAllowList`; config tests `TestProductionRejections`. |
| Supabase JWKS (ES256/RS256) | CONFIGURED, CONNECTED, TESTED (local key server) | `internal/api/server.go:87-88`; `middleware/jwks.go:30` (10 min cache); `TestAuthenticateUserES256ViaJWKS`, `TestProductionJWKSOnlyAllowed`. |
| Supabase Postgres | CONFIGURED, CONNECTED, TESTED (plain Postgres + local auth shim, RLS tests) | `internal/database`, `tests/rls_enforcement_test.go`, `tests/migrations_test.go`. |
| Antigravity AI provider (`agy` subprocess) | CONFIGURED, CONNECTED, TESTED (mock scripts) | `main.go:194-210`; `internal/provider/antigravity/executor.go`; `TestAntigravityExecutor_*`, `TestParseOutput_*`, `TestWorkerAntigravitySuccess`, `TestWorkerAntigravityAuthFailureEvent`. Live test `TestAntigravityLiveIntegration` and `GRIFFIN_ENABLE_LIVE_AGY_TEST` are opt-in, not run. |
| Calendar (`calendar:read/write`, `MODIFY_CALENDAR`) | CONFIGURED only (policy vocabulary) | `internal/domain/capability.go:16-26,42-52,95` define capabilities and approval risk; `service/planner.go:286` keyword routing. No Google Calendar client, OAuth or API call exists. Effectively UNAVAILABLE as an integration. |
| Google / Gmail / email (SES, SMTP, etc.) | UNAVAILABLE | No Go code references any client. (Gmail/Drive MCP connectors are tooling for developers, not Griffin.) |
| Webhooks / Slack / push notifications | UNAVAILABLE | No outbound or inbound webhook handler in `internal/api/server.go` routes. |
| Presigned PUT (direct-to-S3 upload) | UNAVAILABLE | Provider only implements `PresignDownloadURL`; uploads go through the API. |

## Artifacts audit results (this branch)

Verified by `internal/api/handlers/artifacts_test.go` with the mock provider, plus the full suite incl. `tests/` against a throwaway Postgres.
Round trip (multipart and raw), cross-user get/download/presign/delete/list isolation (404, no data), foreign or invalid task_id, presign expiry
and `expires_at`, oversize 413 (multipart, raw, no Content-Length), missing `file` field (400), invalid UUIDs (400), filename sanitisation,
delete of metadata and object, delete failure, storage error mapping.

Bugs found and fixed:
1. Oversize multipart returned 400 and oversize raw body returned 500 with an internal message. Both now return 413 stating the byte limit.
2. Invalid `task_id` (upload, list) was silently ignored, so the artifact was stored unscoped. Now 400.
3. Presign `expires_in` allowed any duration (callers could exceed configured lifetime) and the default ignored `S3_PRESIGN_EXPIRY_MINUTES`. Default now comes from config; `expires_in` can only shorten.
4. Filenames with `\` or `..` uploaded successfully but were then rejected by the key validator, making the artifact undownloadable. Names are sanitised (`SanitizeFilename`).
5. Same filename in the same task produced the same object key, so a second upload overwrote the first and deleting either destroyed both. Keys now carry a UUID segment: `users/{uid}/tasks/{tid}/outputs/{uuid}/{name}`. Existing objects keep their old keys and still work.
6. `Content-Disposition` interpolated the filename unescaped (quote/CRLF injection). Now built with `mime.FormatMediaType`.
7. Delete swallowed object-delete failures and removed metadata, orphaning the object permanently. Now returns 502 and keeps metadata so the delete can be retried. `NoSuchKey` is still treated as success.
8. Error mapping: unclassified errors echoed raw `err.Error()` (could include AWS ARNs/bucket names). They now return a generic message and are logged. Bucket missing is 503 (server misconfiguration, was 404); S3 AccessDenied is 502 (server IAM issue, was 403 blaming the caller).

## Is MAX_UPLOAD_BYTES = 5 MiB safe under the Lambda payload cap?

No. Lambda's synchronous request limit is 6 MB (6,291,456 bytes) for the whole event JSON. An HTTP API delivers binary bodies
base64-encoded (x4/3), so a 5 MiB body becomes about 6.99 MB and is rejected by Lambda before the handler runs, and multipart framing adds more.
Maximum raw body is about 4.5 MiB in theory; allowing for headers and JSON envelope, a safe setting is `MAX_UPLOAD_BYTES=4194304` (4 MiB),
or 3.5 MiB for margin. The default (`config.go:131`) was left unchanged because production may set it via env; recommend lowering it.
For larger files, add a presigned-PUT flow (`POST /artifacts/presign-upload` returning `{url, expires_at, object_key}`, client PUTs directly to S3,
then `POST /artifacts` registers it; `RegisterArtifact` already verifies the key prefix and does a HEAD on the object). A 413 from Lambda itself will not
have Griffin's JSON envelope, so the web client should pre-check size.
Note: no Lambda adapter or payload setting exists in this repo's Go code or infra scripts, so the deployment path could not be confirmed here.

## Test environment note

The default `TestDBURL` in `tests/testhelper.go` points to `localhost:54322` (a local Supabase dev DB named `griffin_test`). Run integration tests
with `GRIFFIN_TEST_DB_URL` pointing at a throwaway container instead.
