import { describe, expect, it } from "vitest";
import type { Task, TaskAttempt } from "@/lib/api";
import { fixtures } from "@/test/fixtures";
import { deriveNow } from "@/lib/task-now";

const task = fixtures.task as Task;
const attempt = (n: number, extra: Partial<TaskAttempt> = {}): TaskAttempt => ({
  id: `a${n}`, task_id: task.id, worker_id: `worker-${n}`, attempt_number: n, status: "FAILED",
  prompt_tokens: 0, completion_tokens: 0, duration_ms: 1, started_at: "2026-11-01T10:10:00Z", ...extra,
});

describe("deriveNow", () => {
  it("maps status to a plain-language stage", () => {
    expect(deriveNow({ ...task, status: "AWAITING_APPROVAL" }, [], []).stage).toBe("Waiting for your approval");
  });
  it("uses the newest attempt's worker and the latest timestamp", () => {
    const n = deriveNow(task, [], [attempt(1), attempt(2, { started_at: "2026-11-02T00:00:00Z" })]);
    expect(n.worker).toBe("worker-2");
    expect(n.lastActivityAt).toBe("2026-11-02T00:00:00Z");
  });
  it("reports retries only when there were some", () => {
    expect(deriveNow(task, [], []).retry).toBeNull();
    expect(deriveNow({ ...task, retry_count: 2, max_retries: 3 }, [], []).retry).toEqual({ count: 2, max: 3 });
  });
  it("falls back to the attempt error for failed tasks and never exposes event messages", () => {
    const n = deriveNow({ ...task, status: "FAILED" }, [{ id: 1, task_id: task.id, event_type: "LOG", message: "secret reasoning", created_at: task.created_at }], [attempt(1, { error_message: "worker crashed" })]);
    expect(n.failureReason).toBe("worker crashed");
    expect(JSON.stringify(n)).not.toContain("secret reasoning");
  });
});

// Production incident 2026-11-03: a coordinating JJ parent showed "Worker: Not assigned yet" while
// its first step was actively being executed by griffin-worker-1.
describe("deriveNow: worker truthfulness", () => {
  const parent = { ...task, manager_domain: "JJ", status: "RUNNING", leased_by: "JJ_ORCHESTRATOR" } as Task;
  const step = (n: number, over: Partial<Task> = {}) =>
    ({ ...task, id: `step-${n}`, parent_id: task.id, manager_domain: "research", ...over }) as Task;

  it("a running task shows the worker that holds its lease", () => {
    const n = deriveNow({ ...task, status: "RUNNING", leased_by: "griffin-worker-1" } as Task, [], []);
    expect(n.worker).toBe("griffin-worker-1");
  });

  it("a queued task with no lease and no attempts honestly has no worker yet", () => {
    expect(deriveNow({ ...task, status: "QUEUED", leased_by: null } as Task, [], []).worker).toBeNull();
  });

  it("a coordinating parent says JJ is coordinating and names the worker running a step", () => {
    const steps = [step(1, { status: "RUNNING", leased_by: "griffin-worker-1" }), step(2, { status: "WAITING_FOR_DEPENDENCY" })];
    const n = deriveNow(parent, [], [], steps);
    expect(n.worker).toBe("JJ coordinating · griffin-worker-1");
    expect(n.stage).toBe("Coordinating: 0 of 2 steps done");
  });

  it("a coordinating parent whose steps are all waiting for a worker says so", () => {
    const steps = [step(1, { status: "QUEUED" }), step(2, { status: "WAITING_FOR_DEPENDENCY" })];
    const n = deriveNow(parent, [], [], steps);
    expect(n.worker).toBe("JJ coordinating");
    expect(n.stage).toBe("Coordinating: 0 of 2 steps done, waiting for a worker");
  });

  it("counts completed steps and never shows the orchestrator lease as a worker", () => {
    const steps = [step(1, { status: "COMPLETED" }), step(2, { status: "RUNNING", leased_by: "griffin-worker-2" })];
    const n = deriveNow(parent, [], [], steps);
    expect(n.stage).toBe("Coordinating: 1 of 2 steps done");
    expect(n.worker).not.toContain("JJ_ORCHESTRATOR");
  });

  it("a finished parent falls back to the plain stage", () => {
    const n = deriveNow({ ...parent, status: "COMPLETED" } as Task, [], [], [step(1, { status: "COMPLETED" })]);
    expect(n.stage).toBe("Finished");
  });
});
