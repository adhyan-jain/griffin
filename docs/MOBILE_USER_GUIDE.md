# Griffin on your phone

> **Status: NOT LIVE.** There is no production URL yet. This guide is ready for when the PWA is deployed and verified; `<URL>` will be the real Vercel HTTPS address.

## Install
- **Android (Chrome):** open `<URL>` → menu ⋮ → **Install app** (or **Add to Home screen**) → Install.
- **iPhone (Safari only):** open `<URL>` → Share → **Add to Home Screen** → Add. (Other iOS browsers cannot install it.)

## Use
1. **Sign in** with the Supabase operator account (sign-ups are disabled; there is no registration).
2. **Submit a task:** use Chat/Tasks, describe the goal; JJ plans it and delegates to managers and specialists.
3. **Progress:** Tasks shows status (queued → running → completed/failed); the runner pill shows whether the cloud runner is stopped or running. A stopped runner wakes automatically when work arrives (allow ~1–2 min for boot).
4. **Approvals:** actions needing consent stay blocked in Approvals until you approve or reject them.
5. **Results/artifacts:** open the task or Artifacts.

## Known limitations
- Task execution requires the Antigravity login on the cloud runner to survive reboots — **not yet proven**; until then tasks may queue without running.
- The runner cannot be put to sleep from the app (operator-only, by design); it powers itself off when idle (~5 min).
- No offline mode (no service worker); needs a connection.

## If something fails
Note the task id and time; check the task's event history in the app. Operators inspect Lambda/worker logs (`journalctl -u griffin-worker`) and the `tasks`/`task_events` tables in Supabase.
