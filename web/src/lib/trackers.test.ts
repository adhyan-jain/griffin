import { describe, expect, it } from "vitest";
import type { Task } from "@/lib/api";
import { groupTrackerTasks, internshipActionMessage } from "@/lib/trackers";

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
