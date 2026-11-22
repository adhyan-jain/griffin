import { describe, expect, it } from "vitest";
import type { Task } from "@/lib/api";
import { TRACKER_TYPES, appliedKeys, applyTaskInput, classifyTask, groupTrackerTasks, internshipActionMessage, trackerPeers } from "@/lib/trackers";

const task = (id: string, over: Partial<Task> = {}, payload: unknown = {}): Task =>
  ({ id, title: id, goal: id, status: "COMPLETED", manager_domain: "research", priority: "NORMAL", created_at: "2026-11-19T10:00:00Z", updated_at: "2026-11-19T10:00:00Z", input_payload: payload, ...over }) as Task;
const isTracker = (t: Task) => /tracker|hackathon/i.test(t.title);

describe("groupTrackerTasks", () => {
  it("a follow-up replaces the tracker it continues instead of making a second card", () => {
    const root = task("r", { title: "HACKATHON TRACKER (daily 08:30)", completed_at: "2026-11-19T13:00:00Z" });
    const follow = task("f", { title: "Follow-up: i dont want a tracker of websites but actual hackathons", completed_at: "2026-11-19T14:20:00Z" }, { thread_id: "r", follow_up_of: "r" });
    const cards = groupTrackerTasks([root, follow], isTracker);
    expect(cards).toHaveLength(1);
    expect(cards[0].id).toBe("f"); // the newer content wins
    expect(cards[0].title).toBe("HACKATHON TRACKER (daily 08:30)"); // under the tracker's own name
  });

  it("an older follow-up never hides a newer scheduled run", () => {
    const root = task("r", { title: "INTERNSHIP TRACKER", completed_at: "2026-11-19T08:00:00Z" });
    const follow = task("f", { title: "Follow-up: add stipend" }, { thread_id: "r", follow_up_of: "r" });
    const rerun = task("n", { title: "INTERNSHIP TRACKER", completed_at: "2026-11-20T08:00:00Z" });
    const cards = groupTrackerTasks([root, follow, rerun], isTracker);
    expect(cards.map((c) => c.id)).toEqual(["n"]);
  });

  it("a follow-up with unrelated wording still joins its tracker's type", () => {
    const root = task("r", { title: "HACKATHON TRACKER", completed_at: "2026-11-19T13:00:00Z" });
    const follow = task("f", { title: "JJ Follow-up: give real events", completed_at: "2026-11-19T14:00:00Z" }, { thread_id: "r", follow_up_of: "r" });
    const cards = groupTrackerTasks([root, follow], isTracker);
    expect(cards.map((c) => c.id)).toEqual(["f"]);
  });

  it("two tasks of one type share a single card", () => {
    const a = task("a", { title: "INTERNSHIP TRACKER (daily)", completed_at: "2026-11-19T08:00:00Z" });
    const b = task("b", { title: "Internship tracker: LinkedIn scan", completed_at: "2026-11-20T08:00:00Z" });
    expect(groupTrackerTasks([a, b], (t) => /internship/i.test(t.title)).map((c) => c.id)).toEqual(["b"]);
  });

  it("a newer task with no output yet never hides the tracker that has data", () => {
    const real = task("real", { title: "INTERNSHIP TRACKER", completed_at: "2026-11-19T08:00:00Z", output_payload: { text: "| Company |\n| Acme |" } as never });
    const queued = task("queued", { title: "INTERNSHIP TRACKER - JOB EMAILS (daily 07:30)", status: "QUEUED", output_payload: null as never, updated_at: "2026-11-20T14:00:00Z" });
    expect(groupTrackerTasks([real, queued], (t) => /internship/i.test(t.title)).map((c) => c.id)).toEqual(["real"]);
  });

  it("different trackers stay separate", () => {
    const a = task("a", { title: "HACKATHON TRACKER" });
    const b = task("b", { title: "INTERNSHIP TRACKER" });
    expect(groupTrackerTasks([a, b], isTracker)).toHaveLength(2);
  });

  it("a follow-up whose root is missing still forms a single card", () => {
    const f1 = task("f1", { title: "Follow-up: hackathon list" }, { thread_id: "gone", follow_up_of: "gone" });
    const f2 = task("f2", { title: "Follow-up: hackathon list v2", completed_at: "2026-11-19T12:00:00Z" }, { thread_id: "gone", follow_up_of: "gone" });
    expect(groupTrackerTasks([f1, f2], (t) => /hackathon/i.test(t.title))).toHaveLength(1);
  });
});

describe("internshipActionMessage", () => {
  it("never asks to send anything and names the role and company", () => {
    const m = internshipActionMessage("draft", "Acme", "SDE Intern");
    expect(m).toContain("SDE Intern at Acme");
    expect(m).toMatch(/Do not send/);
  });
  it("asks for the exact posting URL", () => {
    expect(internshipActionMessage("url", "Acme", "SDE Intern")).toMatch(/exact application URL/);
  });
});

describe("new internship tasks join the one Internships tracker", () => {
  const isTracker = (t: Task) => classifyTask(t) === "internships";
  const tracker = task("t", { title: "INTERNSHIP TRACKER", goal: "weekly list", completed_at: "2026-11-19T08:00:00Z", output_payload: { text: "x" } as never });
  const gather = task("g", { title: "Find SDE roles", goal: "Gather a list of companies hiring interns for summer 2027", completed_at: "2026-11-20T08:00:00Z", output_payload: { text: "y" } as never });

  it("is typed by its goal when the title has no keyword", () => {
    expect(classifyTask(gather)).toBe("internships");
    expect(groupTrackerTasks([tracker, gather], isTracker)).toHaveLength(1);
  });
  it("does not mistake 'internal' for an internship", () => {
    expect(classifyTask(task("i", { title: "Internal audit prep", goal: "check internal docs" }))).not.toBe("internships");
  });
  it("lists every task of the tracker so their rows can be appended", () => {
    const winner = groupTrackerTasks([tracker, gather], isTracker)[0];
    expect(trackerPeers([tracker, gather], isTracker, winner).map((p) => p.id)).toEqual(["t", "g"]);
  });
});

describe("apply tasks", () => {
  const apply = task("a", { title: "APPLY - Acme - SDE Intern", status: "COMPLETED" }, { apply_for: { company: "Acme", role: "SDE Intern", url: null } });
  it("never become a tracker, even with 'intern' in the title", () => {
    expect(groupTrackerTasks([apply], (t) => /intern/i.test(t.title))).toHaveLength(0);
  });
  it("mark their listing applied unless cancelled or failed", () => {
    expect(appliedKeys([apply]).has("acme|sde intern")).toBe(true);
    expect(appliedKeys([{ ...apply, status: "FAILED" } as Task]).size).toBe(0);
  });
  it("wait for approval instead of submitting", () => {
    const input = applyTaskInput("Acme", "SDE Intern", "https://a/1");
    expect(input.goal).toMatch(/Do not submit anything until I approve/);
    expect(input.goal).toContain("https://a/1");
  });
});

describe("the Communities tracker", () => {
  it("is a tracker type, and the daily community scan lands on it", () => {
    expect(TRACKER_TYPES.find((t) => t.id === "community")?.label).toBe("Communities");
    expect(classifyTask(task("c", { title: "COMMUNITY SCAN (daily 09:00)" }))).toBe("community");
  });
  it("shares one card across its runs", () => {
    const a = task("a", { title: "COMMUNITY SCAN", completed_at: "2026-11-21T09:00:00Z" });
    const b = task("b", { title: "COMMUNITY SCAN", completed_at: "2026-11-22T09:00:00Z" });
    expect(groupTrackerTasks([a, b], (t) => classifyTask(t) === "community").map((c) => c.id)).toEqual(["b"]);
  });
});
