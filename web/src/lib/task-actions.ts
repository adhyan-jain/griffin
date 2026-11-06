import { api, type Task } from "@/lib/api";
import { isDone } from "@/components/StatusBadge";

/** A JJ plan root (its own orchestration record) as opposed to a standalone or chat task. */
export const isPlan = (t: Task) => t.manager_domain === "JJ" && (t.title ?? "").startsWith("JJ Orchestration");
const isChat = (t: Task) => (t.input_payload as { kind?: string } | null | undefined)?.kind === "chat";

/** Failed or cancelled standalone tasks continue in place. The server enforces the same rule. */
export const canResume = (t: Task) => (t.status === "FAILED" || t.status === "CANCELLED") && !t.parent_id && !isPlan(t);
/** Plans, and finished work in general, are repeated by submitting the same request again. */
export const canRunAgain = (t: Task) => isDone(t.status) && !t.parent_id && !isChat(t) && (isPlan(t) || t.status === "COMPLETED");
export const canDelete = (t: Task) => isDone(t.status);
/** Only top-level work is cancelled directly; cancelling a plan stops its subtasks too. */
export const canCancel = (t: Task) => !isDone(t.status) && !t.parent_id;

export async function runAgain(t: Task): Promise<Task | null> {
  if (isPlan(t)) {
    await api.submitOrchestratorRequest(t.goal, t.priority);
    return null;
  }
  return api.createTask({ goal: t.goal, title: t.title, manager_domain: t.manager_domain, priority: t.priority });
}

export type Outcome = { ok: number; failed: number; skipped: number; firstError?: string };

/** Runs one action per task with a small concurrency cap; one failure never stops the rest. */
export async function runBulk(tasks: Task[], eligible: (t: Task) => boolean, act: (t: Task) => Promise<unknown>, concurrency = 4): Promise<Outcome> {
  const todo = tasks.filter(eligible);
  const out: Outcome = { ok: 0, failed: 0, skipped: tasks.length - todo.length };
  let next = 0;
  await Promise.all(
    Array.from({ length: Math.min(concurrency, todo.length) }, async () => {
      while (next < todo.length) {
        const t = todo[next++];
        try {
          await act(t);
          out.ok++;
        } catch (e) {
          out.failed++;
          out.firstError ??= e instanceof Error ? e.message : "Request failed";
        }
      }
    }),
  );
  return out;
}

/** Done-tab time windows, newest first. `days: null` means no limit. */
export const PERIODS = [
  { value: "all", label: "All time", days: null },
  { value: "1", label: "Today", days: 1 },
  { value: "7", label: "Past week", days: 7 },
  { value: "30", label: "Past month", days: 30 },
  { value: "60", label: "Past 2 months", days: 60 },
  { value: "90", label: "Past 3 months", days: 90 },
] as const;
export type Period = (typeof PERIODS)[number]["value"];

export function inPeriod(t: Task, period: Period, now = Date.now()): boolean {
  const days = PERIODS.find((p) => p.value === period)?.days;
  if (!days) return true;
  const at = Date.parse(t.completed_at ?? t.updated_at ?? t.created_at);
  return Number.isFinite(at) && now - at <= days * 86_400_000;
}

const normGoal = (s: string) => s.toLowerCase().replace(/\s+/g, " ").trim();

/**
 * A finished, top-level task with the same goal completed recently (default 24h), newest first.
 * Used to offer the existing result before paying for an identical run. Chat and plan subtasks are
 * never offered, and a failed or cancelled run does not count.
 */
export function findRecentResult(tasks: Task[], goal: string, now = Date.now(), maxAgeMs = 24 * 3_600_000): Task | null {
  const want = normGoal(goal);
  if (want.length < 8) return null;
  let best: Task | null = null;
  let bestAt = 0;
  for (const t of tasks) {
    if (t.status !== "COMPLETED" || t.parent_id || isChat(t) || normGoal(t.goal ?? "") !== want) continue;
    const at = Date.parse(t.completed_at ?? t.updated_at);
    if (Number.isNaN(at) || now - at > maxAgeMs || at <= bestAt) continue;
    best = t;
    bestAt = at;
  }
  return best;
}
