import { describe, expect, it } from "vitest";
import type { Task } from "@/lib/api";
import { canCancel, canDelete, canResume, canRunAgain, inPeriod, isPlan, runBulk } from "@/lib/task-actions";

const DAY = 86_400_000;
const NOW = Date.parse("2026-11-06T12:00:00Z");
const task = (over: Partial<Task>): Task =>
  ({
    id: "t", user_id: "u", manager_domain: "research", title: "t", goal: "g", status: "COMPLETED", priority: "NORMAL_P2",
    retry_count: 0, max_retries: 3, created_at: new Date(NOW - 10 * DAY).toISOString(), updated_at: new Date(NOW - 10 * DAY).toISOString(),
    ...over,
  }) as Task;

describe("task action eligibility", () => {
  it("resumes only failed or cancelled standalone tasks", () => {
    expect(canResume(task({ status: "FAILED" }))).toBe(true);
    expect(canResume(task({ status: "CANCELLED" }))).toBe(true);
    expect(canResume(task({ status: "COMPLETED" }))).toBe(false);
    expect(canResume(task({ status: "RUNNING" }))).toBe(false);
    expect(canResume(task({ status: "FAILED", parent_id: "p" }))).toBe(false);
    expect(canResume(task({ status: "FAILED", manager_domain: "JJ", title: "JJ Orchestration: x" }))).toBe(false);
  });

  it("offers run again for plans and completed standalone work, never for chat or subtasks", () => {
    expect(canRunAgain(task({ status: "FAILED", manager_domain: "JJ", title: "JJ Orchestration: x" }))).toBe(true);
    expect(canRunAgain(task({ status: "COMPLETED" }))).toBe(true);
    expect(canRunAgain(task({ status: "FAILED" }))).toBe(false);
    expect(canRunAgain(task({ status: "COMPLETED", input_payload: { kind: "chat" } }))).toBe(false);
    expect(canRunAgain(task({ status: "COMPLETED", parent_id: "p" }))).toBe(false);
    expect(canRunAgain(task({ status: "RUNNING" }))).toBe(false);
  });

  it("deletes only finished tasks", () => {
    for (const s of ["COMPLETED", "FAILED", "CANCELLED"]) expect(canDelete(task({ status: s as Task["status"] }))).toBe(true);
    for (const s of ["RUNNING", "QUEUED", "RETRYING", "AWAITING_APPROVAL"]) expect(canDelete(task({ status: s as Task["status"] }))).toBe(false);
  });

  it("cancels only unfinished top-level work", () => {
    for (const s of ["RUNNING", "QUEUED", "RETRYING", "AWAITING_APPROVAL"]) expect(canCancel(task({ status: s as Task["status"] }))).toBe(true);
    for (const s of ["COMPLETED", "FAILED", "CANCELLED"]) expect(canCancel(task({ status: s as Task["status"] }))).toBe(false);
    expect(canCancel(task({ status: "RUNNING", parent_id: "p" }))).toBe(false);
  });

  it("recognises a plan root by its title and domain", () => {
    expect(isPlan(task({ manager_domain: "JJ", title: "JJ Orchestration: weekly" }))).toBe(true);
    expect(isPlan(task({ manager_domain: "JJ", title: "chat" }))).toBe(false);
  });
});

describe("inPeriod", () => {
  const doneAgo = (d: number) => task({ completed_at: new Date(NOW - d * DAY).toISOString() });
  it("keeps everything for all time", () => expect(inPeriod(doneAgo(400), "all", NOW)).toBe(true));
  it("windows by completion time", () => {
    expect(inPeriod(doneAgo(0.5), "1", NOW)).toBe(true);
    expect(inPeriod(doneAgo(2), "1", NOW)).toBe(false);
    expect(inPeriod(doneAgo(6.9), "7", NOW)).toBe(true);
    expect(inPeriod(doneAgo(7.1), "7", NOW)).toBe(false);
    expect(inPeriod(doneAgo(45), "30", NOW)).toBe(false);
    expect(inPeriod(doneAgo(45), "60", NOW)).toBe(true);
    expect(inPeriod(doneAgo(95), "90", NOW)).toBe(false);
  });
  it("falls back to updated_at when completed_at is missing", () => {
    expect(inPeriod(task({ updated_at: new Date(NOW - 2 * DAY).toISOString() }), "7", NOW)).toBe(true);
  });
  it("excludes a task with an unparseable date from a bounded window", () => {
    expect(inPeriod(task({ completed_at: "not a date" }), "7", NOW)).toBe(false);
  });
});

describe("runBulk", () => {
  it("skips ineligible tasks, counts failures and keeps going after one", async () => {
    const tasks = [task({ id: "a" }), task({ id: "b" }), task({ id: "c" }), task({ id: "d", status: "RUNNING" })];
    const seen: string[] = [];
    const out = await runBulk(
      tasks,
      canDelete,
      async (t) => {
        seen.push(t.id);
        if (t.id === "b") throw new Error("conflict");
      },
      2,
    );
    expect(out).toEqual({ ok: 2, failed: 1, skipped: 1, firstError: "conflict" });
    expect(seen.sort()).toEqual(["a", "b", "c"]);
  });
  it("handles an empty eligible set", async () => {
    expect(await runBulk([task({ status: "RUNNING" })], canDelete, async () => {})).toEqual({ ok: 0, failed: 0, skipped: 1 });
  });
});
