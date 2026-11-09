import { describe, expect, it } from "vitest";
import type { Task } from "@/lib/api";
import { canFollowUp, followUpCounts, followUpsOf, isFollowUp, threadRootId } from "@/lib/threads";

const task = (id: string, over: Partial<Task> = {}, payload: unknown = {}): Task =>
  ({ id, title: id, goal: id, status: "COMPLETED", manager_domain: "JJ", priority: "NORMAL", created_at: "2026-11-09T10:00:00Z", updated_at: "2026-11-09T10:00:00Z", input_payload: payload, ...over }) as Task;
const fu = (id: string, root: string, at: string) => task(id, { created_at: at }, { thread_id: root, follow_up_of: root });

describe("threads", () => {
  it("a task that continues nothing is its own thread root", () => {
    const t = task("a");
    expect(isFollowUp(t)).toBe(false);
    expect(threadRootId(t)).toBe("a");
  });

  it("a follow-up knows its thread and is hidden from the main list", () => {
    const t = fu("b", "a", "2026-11-09T10:05:00Z");
    expect(isFollowUp(t)).toBe(true);
    expect(threadRootId(t)).toBe("a");
  });

  it("collects only this thread's follow-ups, oldest first", () => {
    const all = [task("a"), fu("c", "a", "2026-11-09T10:09:00Z"), fu("b", "a", "2026-11-09T10:05:00Z"), fu("x", "other", "2026-11-09T10:06:00Z"), task("d")];
    expect(followUpsOf(all, "a").map((t) => t.id)).toEqual(["b", "c"]);
    expect(followUpsOf(all, "d")).toEqual([]);
  });

  it("counts follow-ups per thread", () => {
    const all = [task("a"), fu("b", "a", "2026-11-09T10:05:00Z"), fu("c", "a", "2026-11-09T10:06:00Z"), fu("x", "z", "2026-11-09T10:07:00Z")];
    expect(Object.fromEntries(followUpCounts(all))).toEqual({ a: 2, z: 1 });
  });

  it("odd payloads never invent a thread", () => {
    for (const p of [null, undefined, "text", 7, { thread_id: 5, follow_up_of: false }, { thread_id: "" }]) {
      const t = task("a", {}, p);
      expect(isFollowUp(t)).toBe(false);
      expect(threadRootId(t)).toBe("a");
    }
  });

  it("only a finished task can be continued", () => {
    for (const s of ["COMPLETED", "FAILED", "CANCELLED"]) expect(canFollowUp(task("a", { status: s as Task["status"] }))).toBe(true);
    for (const s of ["QUEUED", "RUNNING", "RETRYING", "AWAITING_APPROVAL"]) expect(canFollowUp(task("a", { status: s as Task["status"] }))).toBe(false);
  });
});
