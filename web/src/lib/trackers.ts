import type { Task } from "@/lib/api";
import { isFollowUp, threadRootId } from "@/lib/threads";

/** The known tracker types. A task joins the first type whose pattern matches its title. */
export const TRACKER_TYPES = [
  { id: "internships", label: "Internships", pattern: /internship/i },
  { id: "hackathons", label: "Hackathons", pattern: /hackathon/i },
  { id: "opensource", label: "Open Source", pattern: /open-source|oss/i },
  { id: "hygiene", label: "Repo Hygiene", pattern: /hygiene|repo/i },
  { id: "dsa", label: "DSA Coach", pattern: /dsa/i },
  { id: "reports", label: "Reports & Briefs", pattern: /brief|report|scorecard|audit|thread/i },
] as const;

export const trackerTypeOf = (title: string) => TRACKER_TYPES.find((t) => t.pattern.test(title))?.id ?? null;

/**
 * One tracker per type. Every task of a type (scheduled runs, follow-ups, ad-hoc tasks) updates that
 * type's single card; a follow-up is typed by the tracker it continues, not by its own wording. Only a
 * title matching no known type becomes a tracker of its own. The latest run wins and older versions
 * are hidden, never deleted. A winning follow-up keeps its output but shows under the tracker's name.
 */
export function groupTrackerTasks(all: Task[], isTracker: (t: Task) => boolean): Task[] {
  const byId = new Map(all.map((t) => [t.id, t] as const));
  const rootOf = (t: Task) => (isFollowUp(t) ? byId.get(threadRootId(t)) : undefined);
  const nameOf = (t: Task) => (rootOf(t) ?? t).title.trim() || t.id;
  const keyOf = (t: Task): string => trackerTypeOf(nameOf(t)) ?? nameOf(t);
  const included = (t: Task) => isTracker(t) || (rootOf(t) !== undefined && isTracker(rootOf(t)!));
  const when = (t: Task) => Date.parse(t.completed_at || t.updated_at || t.created_at);
  const grouped = new Map<string, Task>();
  for (const t of all.filter(included)) {
    const key = keyOf(t);
    const existing = grouped.get(key);
    if (!existing || when(t) >= when(existing)) grouped.set(key, t);
  }
  return Array.from(grouped.entries()).map(([key, t]) => {
    if (!isFollowUp(t)) return t;
    const root = rootOf(t);
    return { ...t, title: root ? root.title : key };
  });
}
