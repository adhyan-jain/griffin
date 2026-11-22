import type { Task } from "@/lib/api";
import { isFollowUp, threadRootId } from "@/lib/threads";
import { listingKey } from "@/lib/tracker-rows";

/** The known tracker types. A task joins the first type whose pattern matches its title. */
export const TRACKER_TYPES = [
  { id: "applied", label: "Applied", pattern: /applied tracker/i },
  { id: "alumni", label: "Alumni", pattern: /alumni (tracker|outreach)/i },
  { id: "community", label: "Communities", pattern: /community (scan|tracker)|\bcommunities\b/i },
  { id: "internships", label: "Internships", pattern: /internship|\binterns?\b/i },
  { id: "hackathons", label: "Hackathons", pattern: /hackathon/i },
  { id: "opensource", label: "Open Source", pattern: /open-source|oss/i },
  { id: "hygiene", label: "Repo Hygiene", pattern: /hygiene|repo/i },
  { id: "dsa", label: "DSA Coach", pattern: /dsa/i },
  { id: "reports", label: "Reports & Briefs", pattern: /brief|report|scorecard|audit|thread/i },
] as const;

export const trackerTypeOf = (title: string) => TRACKER_TYPES.find((t) => t.pattern.test(title))?.id ?? null;

type ApplyPayload = { apply_for?: { company?: unknown; role?: unknown; url?: unknown }; submit_for?: unknown } | null | undefined;
const applyOf = (t: Task) => (t.input_payload as ApplyPayload)?.apply_for;
const submitOf = (t: Task) => (t.input_payload as ApplyPayload)?.submit_for;

/** An application task (the draft, or the submission after approval) made from a tracker row. It is work on a listing, never a tracker itself. */
export const isApplyTask = (t: Task) => (applyOf(t) !== undefined && applyOf(t) !== null) || (submitOf(t) !== undefined && submitOf(t) !== null);

/**
 * A task's tracker type: from its title, else from how its goal begins, so "find companies hiring SDE
 * interns" lands on Internships without the word in its title. Only the specific types are read from a
 * goal; the broad ones (reports, repo hygiene) would match too much.
 */
export function classifyTask(t: Task): string | null {
  const byTitle = trackerTypeOf(t.title);
  if (byTitle || isApplyTask(t)) return byTitle;
  const byGoal = trackerTypeOf((t.goal ?? "").slice(0, 200));
  return byGoal === "internships" || byGoal === "hackathons" || byGoal === "opensource" ? byGoal : null;
}

/** Which listings have an application started or done: derived from the apply tasks, never stored apart. */
export function appliedKeys(all: Task[]): Set<string> {
  const keys = new Set<string>();
  for (const t of all) {
    const a = applyOf(t);
    if (a && t.status !== "CANCELLED" && t.status !== "FAILED") keys.add(listingKey(String(a.company ?? ""), String(a.role ?? "")));
  }
  return keys;
}

/** The task Apply creates. It prepares the package and waits for approval; it never submits by itself. */
export function applyTaskInput(company: string, role: string, url: string | null) {
  const what = [role, company && `at ${company}`].filter(Boolean).join(" ");
  return {
    title: `APPLY - ${company} - ${role}`.slice(0, 80),
    goal:
      `Apply for ${what}${url ? ` using this exact posting: ${url}` : " (find the exact posting URL first)"}. ` +
      `Tailor the resume and write the cover note from my profile, fill the application, then stop and ask for my approval. ` +
      `Do not submit anything until I approve it.`,
    manager_domain: "career",
    input_payload: { apply_for: { company, role, url } },
  };
}

/**
 * One tracker per type. Every task of a type (scheduled runs, follow-ups, ad-hoc tasks) updates that
 * type's single card; a follow-up is typed by the tracker it continues, not by its own wording. Only a
 * title matching no known type becomes a tracker of its own. The latest run wins and older versions
 * are hidden, never deleted. A winning follow-up keeps its output but shows under the tracker's name.
 */
function groupContext(all: Task[], isTracker: (t: Task) => boolean) {
  const byId = new Map(all.map((t) => [t.id, t] as const));
  const rootOf = (t: Task) => (isFollowUp(t) ? byId.get(threadRootId(t)) : undefined);
  const nameOf = (t: Task) => (rootOf(t) ?? t).title.trim() || t.id;
  const keyOf = (t: Task): string => classifyTask(rootOf(t) ?? t) ?? nameOf(t);
  const included = (t: Task) => !isApplyTask(t) && (isTracker(t) || (rootOf(t) !== undefined && isTracker(rootOf(t)!)));
  // A task that has produced nothing yet (queued, running, failed) never hides one that has.
  const hasOutput = (t: Task) => t.output_payload != null && Object.keys(t.output_payload as object).length > 0;
  const beats = (a: Task, b: Task) => (hasOutput(a) !== hasOutput(b) ? hasOutput(a) : when(a) >= when(b));
  const when = (t: Task) => Date.parse(t.completed_at || t.updated_at || t.created_at);
  return { rootOf, keyOf, included, beats, when };
}

export function groupTrackerTasks(all: Task[], isTracker: (t: Task) => boolean): Task[] {
  const { rootOf, keyOf, included, beats } = groupContext(all, isTracker);
  const grouped = new Map<string, Task>();
  for (const t of all.filter(included)) {
    const key = keyOf(t);
    const existing = grouped.get(key);
    if (!existing || beats(t, existing)) grouped.set(key, t);
  }
  return Array.from(grouped.entries()).map(([key, t]) => {
    if (!isFollowUp(t)) return t;
    const root = rootOf(t);
    return { ...t, title: root ? root.title : key };
  });
}

/** Every task that belongs to the same tracker as `winner`, oldest first, so their rows can be appended. */
export function trackerPeers(all: Task[], isTracker: (t: Task) => boolean, winner: Task): Task[] {
  const { keyOf, included, when } = groupContext(all, isTracker);
  const key = keyOf(winner);
  return all.filter((t) => included(t) && keyOf(t) === key).sort((a, b) => when(a) - when(b));
}

export type InternshipAction = "draft" | "plan" | "url";

/** The follow-up message an internship row's button sends to its tracker. Nothing is submitted: a draft is only a draft until approved. */
export function internshipActionMessage(action: InternshipAction, company: string, role: string): string {
  const what = [role, company && `at ${company}`].filter(Boolean).join(" ");
  switch (action) {
    case "draft":
      return `Draft the application for ${what}: a short cover note and a tailored resume summary from my profile. Do not send anything; leave it for my approval.`;
    case "plan":
      return `Make a step-by-step application plan for ${what}: deadline, what to prepare, who to reach out to, and the order to do it in.`;
    case "url":
      return `Find the exact application URL for ${what} (the posting itself, not a job-board search page) and update this tracker row with it.`;
  }
}
