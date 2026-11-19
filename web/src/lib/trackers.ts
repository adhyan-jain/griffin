import type { Task } from "@/lib/api";
import { isFollowUp, threadRootId } from "@/lib/threads";

/**
 * One tracker per thread. A follow-up joins the tracker it continues (keyed by its root's title), so
 * it updates that tracker instead of appearing as a second card. The latest run in a group wins and
 * older versions are hidden, never deleted. A winning follow-up keeps its output but shows under the
 * tracker's own name.
 */
export function groupTrackerTasks(all: Task[], isTracker: (t: Task) => boolean): Task[] {
  const byId = new Map(all.map((t) => [t.id, t] as const));
  const keyOf = (t: Task): string => {
    const own = t.title.trim() || t.id;
    if (!isFollowUp(t)) return own;
    const root = byId.get(threadRootId(t));
    return root ? root.title.trim() || root.id : threadRootId(t);
  };
  const when = (t: Task) => Date.parse(t.completed_at || t.updated_at || t.created_at);
  const grouped = new Map<string, Task>();
  for (const t of all.filter(isTracker)) {
    const key = keyOf(t);
    const existing = grouped.get(key);
    if (!existing || when(t) >= when(existing)) grouped.set(key, t);
  }
  return Array.from(grouped.entries()).map(([key, t]) => {
    if (!isFollowUp(t)) return t;
    const root = byId.get(threadRootId(t));
    return { ...t, title: root ? root.title : key };
  });
}
