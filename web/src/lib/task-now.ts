import type { Task, TaskAttempt, TaskEvent, TaskStatus } from "@/lib/api";

export interface TaskNow {
  stage: string;
  lastActivityAt: string;
  manager: string;
  worker: string | null;
  retry: { count: number; max: number } | null;
  failureReason: string | null;
}

const STAGE: Record<TaskStatus, string> = {
  CREATED: "Created, waiting to be routed",
  ROUTED: "Routed to a manager",
  QUEUED: "Queued for a worker",
  RUNNING: "Working on it",
  WAITING_FOR_DEPENDENCY: "Waiting for an earlier step",
  VERIFYING: "Checking the result",
  AWAITING_APPROVAL: "Waiting for your approval",
  RETRYING: "Retrying after a problem",
  COMPLETED: "Finished",
  FAILED: "Stopped with an error",
  CANCELLED: "Cancelled",
};

/**
 * Summarises where a task is from its status, events and attempts. Only structured fields are used:
 * event messages and model output are never surfaced here.
 */
export function deriveNow(task: Task, events: TaskEvent[], attempts: TaskAttempt[]): TaskNow {
  const stamps = [task.updated_at, ...events.map((e) => e.created_at), ...attempts.map((a) => a.completed_at ?? a.started_at)];
  const lastActivityAt = stamps.reduce((a, b) => (new Date(b).getTime() > new Date(a).getTime() ? b : a));
  const latest = [...attempts].sort((a, b) => b.attempt_number - a.attempt_number)[0];
  const payload = task.input_payload as { worker_key?: unknown } | null | undefined;
  const worker = typeof payload?.worker_key === "string" ? payload.worker_key : (latest?.worker_id ?? null);
  return {
    stage: STAGE[task.status] ?? task.status,
    lastActivityAt,
    manager: task.manager_domain,
    worker,
    retry: task.retry_count > 0 ? { count: task.retry_count, max: task.max_retries } : null,
    failureReason: task.failure_reason || (task.status === "FAILED" ? (latest?.error_message ?? null) : null),
  };
}
