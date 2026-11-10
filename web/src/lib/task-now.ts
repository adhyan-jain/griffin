import type { Task, TaskAttempt, TaskEvent, TaskStatus } from "@/lib/api";
import { displayWorker, waitingForDevice } from "@/lib/hosts";

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
const ORCHESTRATOR_LEASE = "JJ_ORCHESTRATOR";
const EXECUTING: TaskStatus[] = ["RUNNING", "VERIFYING"];

export function deriveNow(task: Task, events: TaskEvent[], attempts: TaskAttempt[], children: Task[] = []): TaskNow {
  const stamps = [task.updated_at, ...events.map((e) => e.created_at), ...attempts.map((a) => a.completed_at ?? a.started_at)];
  const lastActivityAt = stamps.reduce((a, b) => (new Date(b).getTime() > new Date(a).getTime() ? b : a));
  const latest = [...attempts].sort((a, b) => b.attempt_number - a.attempt_number)[0];
  const payload = task.input_payload as { worker_key?: unknown } | null | undefined;

  // A JJ parent is not executed by a worker: it coordinates its steps. Say that, and name the worker
  // that is actually running a step, instead of claiming "not assigned".
  const coordinating = children.length > 0 && !["COMPLETED", "FAILED", "CANCELLED"].includes(task.status);
  let worker: string | null;
  let stage = STAGE[task.status] ?? task.status;
  if (coordinating) {
    const running = children.find((c) => EXECUTING.includes(c.status) && c.leased_by && c.leased_by !== ORCHESTRATOR_LEASE);
    worker = running ? `JJ coordinating · ${displayWorker(running.leased_by)}` : "JJ coordinating";
    if (task.status === "RUNNING") {
      const done = children.filter((c) => c.status === "COMPLETED").length;
      const idle = !children.some((c) => EXECUTING.includes(c.status) || c.status === "RETRYING");
      const queued = children.some((c) => c.status === "QUEUED" || c.status === "CREATED" || c.status === "ROUTED");
      const waitingChild = children.find((c) => (c.status === "QUEUED" || c.status === "RETRYING") && waitingForDevice(c));
      const waitingDev = waitingChild ? waitingForDevice(waitingChild) : null;
      stage = `Coordinating: ${done} of ${children.length} steps done${waitingDev ? `, waiting for ${waitingDev} to come online` : idle && queued ? ", waiting for a worker" : ""}`;
    }
  } else {
    const lease = EXECUTING.includes(task.status) && task.leased_by && task.leased_by !== ORCHESTRATOR_LEASE ? task.leased_by : null;
    worker = lease ?? latest?.worker_id ?? (typeof payload?.worker_key === "string" ? payload.worker_key : null);
  }
  // A step that needs a device the owner has not got online is waiting for it, not for a worker.
  const device = coordinating ? null : waitingForDevice(task);
  if (device) stage = `Waiting for ${device} to come online`;
  return {
    stage,
    lastActivityAt,
    manager: task.manager_domain,
    worker: displayWorker(worker),
    retry: task.retry_count > 0 ? { count: task.retry_count, max: task.max_retries } : null,
    failureReason: task.failure_reason || (task.status === "FAILED" ? (latest?.error_message ?? null) : null),
  };
}
