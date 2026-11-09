import type { Task } from "@/lib/api";

/**
 * Threads: a task that is continued ("also add X", "make it shorter") joins the thread of the task it
 * continues instead of appearing as a separate task. The server records this in two payload fields.
 */
type ThreadPayload = { thread_id?: unknown; follow_up_of?: unknown } | null | undefined;
const payloadOf = (t: Task) => t.input_payload as ThreadPayload;
const str = (v: unknown) => (typeof v === "string" && v ? v : null);

/** True when the task continues another one, so it belongs inside that task's thread. */
export const isFollowUp = (t: Task) => str(payloadOf(t)?.follow_up_of) !== null;

/** The id of the thread's first task (the task itself when it is not a follow-up). */
export const threadRootId = (t: Task) => str(payloadOf(t)?.thread_id) ?? t.id;

/** The follow-ups in a thread, oldest first. */
export function followUpsOf(tasks: Task[], rootId: string): Task[] {
  return tasks
    .filter((t) => t.id !== rootId && isFollowUp(t) && threadRootId(t) === rootId)
    .sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at));
}

/** How many follow-ups each thread root has, for a "+2 follow-ups" hint on the task list. */
export function followUpCounts(tasks: Task[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const t of tasks) if (isFollowUp(t)) counts.set(threadRootId(t), (counts.get(threadRootId(t)) ?? 0) + 1);
  return counts;
}

const DONE = new Set(["COMPLETED", "FAILED", "CANCELLED"]);
/** A task can be continued once it has finished (the server refuses otherwise). */
export const canFollowUp = (t: Task) => DONE.has(t.status);
